// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/OpenListTeam/sftpd-openlist"
	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/dongdio/OpenList/global"
	"github.com/dongdio/OpenList/initialize"
	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/fs"
	"github.com/dongdio/OpenList/server"
	"github.com/dongdio/OpenList/server/middlewares"
	"github.com/dongdio/OpenList/utility/utils"
)

// ServerCmd represents the server command that starts the OpenList server
var ServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the OpenList server",
	Long: `Start the OpenList server with HTTP, HTTPS, Unix socket, FTP, SFTP, 
and S3-compatible APIs as configured in the configuration file.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize the application with server mode
		initialize.InitApp(true)

		// Handle delayed start if configured
		if conf.Conf.DelayedStart > 0 {
			delaySeconds := conf.Conf.DelayedStart
			utils.Log.Infof("Configured delayed start: waiting for %d seconds before startup", delaySeconds)
			time.Sleep(time.Duration(delaySeconds) * time.Second)
		}

		// Set Gin mode based on environment
		if !global.Debug && !global.Dev {
			gin.SetMode(gin.ReleaseMode)
			utils.Log.Info("Running in production mode")
		} else if global.Debug {
			utils.Log.Info("Running in debug mode")
		} else if global.Dev {
			utils.Log.Info("Running in development mode")
		}

		// Create and configure Gin router
		r := gin.New()

		// Add middleware for error logging and recovery
		r.Use(middlewares.ErrorLogging())
		r.Use(
			gin.LoggerWithWriter(log.StandardLogger().Out),
			gin.RecoveryWithWriter(log.StandardLogger().Out),
		)

		server.Init(r)

		// Configure HTTP handler with H2C support if enabled
		var httpHandler http.Handler = r
		if conf.Conf.Scheme.EnableH2c {
			utils.Log.Debug("Enabling H2C (HTTP/2 over cleartext) support")
			httpHandler = h2c.NewHandler(r, &http2.Server{})
		}

		// Initialize server variables
		var httpSrv, httpsSrv, unixSrv *http.Server

		// Start HTTP server if configured
		if conf.Conf.Scheme.HttpPort != -1 {
			httpAddr := fmt.Sprintf("%s:%d", conf.Conf.Scheme.Address, conf.Conf.Scheme.HttpPort)
			utils.Log.Infof("Starting HTTP server on %s", httpAddr)

			// Configure HTTP server
			httpSrv = &http.Server{
				Addr:         httpAddr,
				Handler:      httpHandler,
				ReadTimeout:  60 * time.Second,
				WriteTimeout: 60 * time.Second,
				IdleTimeout:  120 * time.Second,
			}

			// Start HTTP server in a goroutine
			go func() {
				if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					utils.Log.Fatalf("Failed to start HTTP server: %v", err)
				}
			}()
		}

		// Start HTTPS server if configured
		if conf.Conf.Scheme.HttpsPort != -1 {
			httpsAddr := fmt.Sprintf("%s:%d", conf.Conf.Scheme.Address, conf.Conf.Scheme.HttpsPort)
			utils.Log.Infof("Starting HTTPS server on %s", httpsAddr)

			// Check if certificate and key files exist
			if !utils.Exists(conf.Conf.Scheme.CertFile) || !utils.Exists(conf.Conf.Scheme.KeyFile) {
				utils.Log.Errorf("Certificate file or key file not found: %s, %s",
					conf.Conf.Scheme.CertFile, conf.Conf.Scheme.KeyFile)
				utils.Log.Warn("HTTPS server will not start due to missing certificate files")
			} else {
				// Configure HTTPS server
				httpsSrv = &http.Server{
					Addr:         httpsAddr,
					Handler:      r, // Use original handler without H2C for HTTPS
					ReadTimeout:  60 * time.Second,
					WriteTimeout: 60 * time.Second,
					IdleTimeout:  120 * time.Second,
				}

				// Start HTTPS server in a goroutine
				go func() {
					if err := httpsSrv.ListenAndServeTLS(
						conf.Conf.Scheme.CertFile,
						conf.Conf.Scheme.KeyFile,
					); err != nil && !errors.Is(err, http.ErrServerClosed) {
						utils.Log.Fatalf("Failed to start HTTPS server: %v", err)
					}
				}()
			}
		}
		// Start Unix socket server if configured
		if conf.Conf.Scheme.UnixFile != "" {
			unixSocketPath := conf.Conf.Scheme.UnixFile
			utils.Log.Infof("Starting Unix socket server on %s", unixSocketPath)

			// Remove existing socket file if it exists
			if utils.Exists(unixSocketPath) {
				if err := os.Remove(unixSocketPath); err != nil {
					utils.Log.Warnf("Failed to remove existing Unix socket file: %v", err)
				}
			}

			// Configure Unix socket server
			unixSrv = &http.Server{
				Handler:      httpHandler,
				ReadTimeout:  60 * time.Second,
				WriteTimeout: 60 * time.Second,
				IdleTimeout:  120 * time.Second,
			}

			// Start Unix socket server in a goroutine
			go func() {
				// Create Unix socket listener
				listener, err := net.Listen("unix", unixSocketPath)
				if err != nil {
					utils.Log.Fatalf("Failed to create Unix socket listener: %v", err)
					return
				}

				// Set socket file permissions
				mode, err := strconv.ParseUint(conf.Conf.Scheme.UnixFilePerm, 8, 32)
				if err != nil {
					utils.Log.Errorf("Failed to parse Unix socket file permission '%s': %v",
						conf.Conf.Scheme.UnixFilePerm, err)
				} else {
					if err = os.Chmod(unixSocketPath, os.FileMode(mode)); err != nil {
						utils.Log.Errorf("Failed to set Unix socket file permissions: %v", err)
					} else {
						utils.Log.Debugf("Set Unix socket file permissions to %s", conf.Conf.Scheme.UnixFilePerm)
					}
				}

				// Start serving on the Unix socket
				if err = unixSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					utils.Log.Fatalf("Failed to start Unix socket server: %v", err)
				}
			}()
		}
		// Start S3-compatible API server if enabled
		if conf.Conf.S3.Port != -1 && conf.Conf.S3.Enable {
			// Create a new Gin router for S3 API
			s3Router := gin.New()
			s3Router.Use(
				gin.LoggerWithWriter(log.StandardLogger().Out),
				gin.RecoveryWithWriter(log.StandardLogger().Out),
			)

			// Initialize S3 API routes
			server.InitS3(s3Router)

			// Configure S3 server address
			s3Addr := fmt.Sprintf("%s:%d", conf.Conf.Scheme.Address, conf.Conf.S3.Port)
			utils.Log.Infof("Starting S3-compatible API server on %s (SSL: %v)",
				s3Addr, conf.Conf.S3.SSL)

			// Start S3 server in a goroutine
			go func() {
				var err error
				var s3Server *http.Server

				// Configure the S3 server with appropriate timeouts
				s3Server = &http.Server{
					Addr:    s3Addr,
					Handler: s3Router,
					// Increase timeouts for S3 operations which may involve large files
					ReadTimeout:  5 * time.Minute,
					WriteTimeout: 5 * time.Minute,
					IdleTimeout:  120 * time.Second,
				}

				// Start with SSL if configured
				if conf.Conf.S3.SSL {
					// Check if certificate and key files exist
					if !utils.Exists(conf.Conf.Scheme.CertFile) || !utils.Exists(conf.Conf.Scheme.KeyFile) {
						utils.Log.Errorf("Certificate file or key file not found for S3 SSL: %s, %s",
							conf.Conf.Scheme.CertFile, conf.Conf.Scheme.KeyFile)
						utils.Log.Warn("S3 server will start without SSL despite configuration")

						// Fall back to non-SSL
						err = s3Server.ListenAndServe()
					} else {
						// Start with SSL
						err = s3Server.ListenAndServeTLS(conf.Conf.Scheme.CertFile, conf.Conf.Scheme.KeyFile)
					}
				} else {
					// Start without SSL
					err = s3Server.ListenAndServe()
				}

				// Check for server errors
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					utils.Log.Fatalf("Failed to start S3-compatible API server: %v", err)
				}
			}()
		}
		// Initialize FTP server components
		var ftpDriver *server.FtpMainDriver
		var ftpServer *ftpserver.FtpServer

		// Start FTP server if enabled
		if conf.Conf.FTP.Listen != "" && conf.Conf.FTP.Enable {
			utils.Log.Info("Initializing FTP server...")

			// Create FTP driver
			var err error
			ftpDriver, err = server.NewMainDriver()
			if err != nil {
				utils.Log.Fatalf("Failed to initialize FTP driver: %v", err)
			} else {
				utils.Log.Infof("Starting FTP server on %s", conf.Conf.FTP.Listen)

				// Start FTP server in a goroutine
				go func() {
					// Create and configure FTP server
					ftpServer = ftpserver.NewFtpServer(ftpDriver)

					// Start listening for FTP connections
					if err := ftpServer.ListenAndServe(); err != nil {
						utils.Log.Fatalf("FTP server error: %v", err)
					}
				}()
			}
		}

		// Initialize SFTP server components
		var sftpDriver *server.SftpDriver
		var sftpServer *sftpd.SftpServer

		// Start SFTP server if enabled
		if conf.Conf.SFTP.Listen != "" && conf.Conf.SFTP.Enable {
			utils.Log.Info("Initializing SFTP server...")

			// Create SFTP driver
			var err error
			sftpDriver, err = server.NewSftpDriver()
			if err != nil {
				utils.Log.Fatalf("Failed to initialize SFTP driver: %v", err)
			} else {
				utils.Log.Infof("Starting SFTP server on %s", conf.Conf.SFTP.Listen)

				// Start SFTP server in a goroutine
				go func() {
					// Create and configure SFTP server
					sftpServer = sftpd.NewSftpServer(sftpDriver)

					// Start listening for SFTP connections
					if err := sftpServer.RunServer(); err != nil {
						utils.Log.Fatalf("SFTP server error: %v", err)
					}
				}()
			}
		}
		// Set up graceful shutdown on interrupt signals
		// Wait for interrupt signal to gracefully shutdown all servers
		quit := make(chan os.Signal, 1)

		// Register for SIGINT (Ctrl+C) and SIGTERM (kill) signals
		// Note: SIGKILL cannot be caught, so we don't need to handle it
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		// Block until a signal is received
		<-quit
		utils.Log.Info("Shutdown signal received, gracefully shutting down servers...")

		// Clean up tasks and release resources
		fs.ArchiveContentUploadTaskManager.RemoveAll()
		Release()

		// Create context with timeout for graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Use WaitGroup to wait for all servers to shutdown
		var wg sync.WaitGroup

		// Shutdown HTTP server if enabled
		if conf.Conf.Scheme.HttpPort != -1 && httpSrv != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				utils.Log.Debug("Shutting down HTTP server...")
				if err := httpSrv.Shutdown(ctx); err != nil {
					utils.Log.Errorf("HTTP server shutdown error: %v", err)
				}
			}()
		}

		// Shutdown HTTPS server if enabled
		if conf.Conf.Scheme.HttpsPort != -1 && httpsSrv != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				utils.Log.Debug("Shutting down HTTPS server...")
				if err := httpsSrv.Shutdown(ctx); err != nil {
					utils.Log.Errorf("HTTPS server shutdown error: %v", err)
				}
			}()
		}

		// Shutdown Unix socket server if enabled
		if conf.Conf.Scheme.UnixFile != "" && unixSrv != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				utils.Log.Debug("Shutting down Unix socket server...")
				if err := unixSrv.Shutdown(ctx); err != nil {
					utils.Log.Errorf("Unix server shutdown error: %v", err)
				}
			}()
		}

		// Shutdown FTP server if enabled
		if conf.Conf.FTP.Listen != "" && conf.Conf.FTP.Enable && ftpServer != nil && ftpDriver != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				utils.Log.Debug("Shutting down FTP server...")
				ftpDriver.Stop()
				if err := ftpServer.Stop(); err != nil {
					utils.Log.Errorf("FTP server shutdown error: %v", err)
				}
			}()
		}

		// Shutdown SFTP server if enabled
		if conf.Conf.SFTP.Listen != "" && conf.Conf.SFTP.Enable && sftpServer != nil && sftpDriver != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				utils.Log.Debug("Shutting down SFTP server...")
				if err := sftpServer.Close(); err != nil {
					utils.Log.Errorf("SFTP server shutdown error: %v", err)
				}
			}()
		}

		// Wait for all servers to finish shutting down
		wg.Wait()
		utils.Log.Info("All servers successfully shut down")
	},
}

// init registers the server command with the root command
func init() {
	RootCmd.AddCommand(ServerCmd)
}

// OutOpenListInit provides a public function to start the server from external code
// This can be used by other packages to initialize the OpenList server
func OutOpenListInit() {
	// Create empty command and args to pass to the Run function
	var (
		cmd  *cobra.Command
		args []string
	)

	// Run the server command
	ServerCmd.Run(cmd, args)
}