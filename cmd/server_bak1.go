// Package cmd implements command-line functionality for OpenList
package cmd

//
// import (
// 	"context"
// 	"errors"
// 	"fmt"
// 	"net"
// 	"net/http"
// 	"os"
// 	"os/signal"
// 	"strconv"
// 	"sync"
// 	"syscall"
// 	"time"
//
// 	"github.com/OpenListTeam/sftpd-openlist"
// 	ftpserver "github.com/fclairamb/ftpserverlib"
// 	"github.com/gogf/gf/v2/frame/g"
// 	"github.com/gogf/gf/v2/net/ghttp"
// 	"github.com/gogf/gf/v2/os/glog"
// 	"github.com/spf13/cobra"
//
// 	"github.com/dongdio/OpenList/v4/global"
// 	"github.com/dongdio/OpenList/v4/initialize"
// 	"github.com/dongdio/OpenList/v4/internal/conf"
// 	"github.com/dongdio/OpenList/v4/internal/fs"
// 	"github.com/dongdio/OpenList/v4/server"
// 	"github.com/dongdio/OpenList/v4/utility/utils"
// )
//
// // Constants for server operation
// const (
// 	// GracefulShutdownTimeout is the time to wait for server shutdown before forcing
// 	GracefulShutdownTimeout = 30 * time.Second
// 	// DefaultHTTPTimeout for HTTP operations
// 	DefaultHTTPTimeout = 60 * time.Second
// 	// DefaultIdleTimeout for HTTP connections
// 	DefaultIdleTimeout = 120 * time.Second
// 	// S3OperationTimeout for S3 operations which may involve large files
// 	S3OperationTimeout = 5 * time.Minute
// )
//
// // ServerManager manages all server instances
// type ServerManager struct {
// 	httpServer  *http.Server
// 	httpsServer *http.Server
// 	unixServer  *http.Server
// 	s3Server    *http.Server
// 	ftpServer   *ftpserver.FtpServer
// 	sftpServer  *sftpd.SftpServer
// 	ftpDriver   *server.FtpMainDriver
// 	sftpDriver  *server.SftpDriver
// }
//
// // ServerCmd_v2 represents the server command that starts the OpenList server
// var ServerCmd_v2 = &cobra.Command{
// 	Use:   "server_v2",
// 	Short: "Start the OpenList server",
// 	Long: `Start the OpenList server with HTTP, HTTPS, Unix socket, FTP, SFTP,
// and S3-compatible APIs as configured in the configuration file.`,
// 	Run: runServerCommand,
// }
//
// func init() {
// 	RootCmd.AddCommand(ServerCmd_v2)
// }
//
// // runServerCommand executes the server command
// func runServerCommand(cmd *cobra.Command, args []string) {
// 	// Initialize the application with server mode
// 	initialize.InitApp(true)
//
// 	// Handle delayed start if configured
// 	handleDelayedStart()
//
// 	// Configure logging based on environment
// 	configureLogging()
//
// 	// Create and configure GoFrame server
// 	mainServer := g.Server()
// 	setupMainServer(mainServer)
//
// 	// Initialize server manager
// 	serverManager := &ServerManager{}
//
// 	// Start all configured servers
// 	startAllServers(mainServer, serverManager)
//
// 	// Setup graceful shutdown
// 	setupGracefulShutdown(serverManager)
// }
//
// // handleDelayedStart handles delayed startup if configured
// func handleDelayedStart() {
// 	if conf.Conf.DelayedStart > 0 {
// 		delaySeconds := conf.Conf.DelayedStart
// 		utils.Log.Infof("Configured delayed start: waiting for %d seconds before startup", delaySeconds)
// 		time.Sleep(time.Duration(delaySeconds) * time.Second)
// 	}
// }
//
// // configureLogging sets up logging level based on environment
// func configureLogging() {
// 	switch {
// 	case !global.Debug && !global.Dev:
// 		glog.SetLevel(glog.LEVEL_INFO)
// 		utils.Log.Info("Running in production mode")
// 	case global.Debug:
// 		glog.SetLevel(glog.LEVEL_ALL)
// 		utils.Log.Info("Running in debug mode")
// 	case global.Dev:
// 		glog.SetLevel(glog.LEVEL_DEBU)
// 		utils.Log.Info("Running in development mode")
// 	}
// }
//
// // setupMainServer configures the main GoFrame server
// func setupMainServer(mainServer *ghttp.Server) {
// 	// Add middleware for error logging and recovery
// 	mainServer.Use(ghttp.MiddlewareCORS, ghttp.MiddlewareGzip, ghttp.MiddlewareHandlerResponse)
// 	mainServer.Group("/", server.Init_bak)
// }
//
// // startAllServers starts all configured server instances
// func startAllServers(mainServer *ghttp.Server, serverManager *ServerManager) {
// 	// Configure HTTP handler with H2C support if enabled
// 	var httpHandler http.Handler = mainServer
// 	// if conf.Conf.Scheme.EnableH2c {
// 	// 	utils.Log.Debug("Enabling H2C (HTTP/2 over cleartext) support")
// 	// 	httpHandler = h2c.NewHandler(mainServer, &http2.Server{})
// 	// }
//
// 	// Start HTTP server
// 	serverManager.httpServer = startHTTPServer(httpHandler)
//
// 	// Start HTTPS server
// 	serverManager.httpsServer = startHTTPSServer(mainServer)
//
// 	// Start Unix socket server
// 	serverManager.unixServer = startUnixSocketServer(httpHandler)
//
// 	// Start S3-compatible API server
// 	serverManager.s3Server = startS3Server()
//
// 	// Start FTP server
// 	serverManager.ftpServer, serverManager.ftpDriver = startFTPServer()
//
// 	// Start SFTP server
// 	serverManager.sftpServer, serverManager.sftpDriver = startSFTPServer()
// }
//
// // startHTTPServer starts the HTTP server if configured
// func startHTTPServer(handler http.Handler) *http.Server {
// 	if conf.Conf.Scheme.HttpPort == -1 {
// 		return nil
// 	}
//
// 	httpAddr := fmt.Sprintf("%s:%d", conf.Conf.Scheme.Address, conf.Conf.Scheme.HttpPort)
// 	utils.Log.Infof("Starting HTTP server on %s", httpAddr)
//
// 	httpServer := &http.Server{
// 		Addr:         httpAddr,
// 		Handler:      handler,
// 		ReadTimeout:  DefaultHTTPTimeout,
// 		WriteTimeout: DefaultHTTPTimeout,
// 		IdleTimeout:  DefaultIdleTimeout,
// 	}
//
// 	go func() {
// 		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
// 			utils.Log.Fatalf("Failed to start HTTP server: %v", err)
// 		}
// 	}()
//
// 	return httpServer
// }
//
// // startHTTPSServer starts the HTTPS server if configured
// func startHTTPSServer(handler http.Handler) *http.Server {
// 	if conf.Conf.Scheme.HttpsPort == -1 {
// 		return nil
// 	}
//
// 	httpsAddr := fmt.Sprintf("%s:%d", conf.Conf.Scheme.Address, conf.Conf.Scheme.HttpsPort)
// 	utils.Log.Infof("Starting HTTPS server on %s", httpsAddr)
//
// 	// Check if certificate and key files exist
// 	if !utils.Exists(conf.Conf.Scheme.CertFile) || !utils.Exists(conf.Conf.Scheme.KeyFile) {
// 		utils.Log.Errorf("Certificate file or key file not found: %s, %s",
// 			conf.Conf.Scheme.CertFile, conf.Conf.Scheme.KeyFile)
// 		utils.Log.Warn("HTTPS server will not start due to missing certificate files")
// 		return nil
// 	}
//
// 	httpsServer := &http.Server{
// 		Addr:         httpsAddr,
// 		Handler:      handler,
// 		ReadTimeout:  DefaultHTTPTimeout,
// 		WriteTimeout: DefaultHTTPTimeout,
// 		IdleTimeout:  DefaultIdleTimeout,
// 	}
//
// 	go func() {
// 		if err := httpsServer.ListenAndServeTLS(
// 			conf.Conf.Scheme.CertFile,
// 			conf.Conf.Scheme.KeyFile,
// 		); err != nil && !errors.Is(err, http.ErrServerClosed) {
// 			utils.Log.Fatalf("Failed to start HTTPS server: %v", err)
// 		}
// 	}()
//
// 	return httpsServer
// }
//
// // startUnixSocketServer starts the Unix socket server if configured
// func startUnixSocketServer(handler http.Handler) *http.Server {
// 	if conf.Conf.Scheme.UnixFile == "" {
// 		return nil
// 	}
//
// 	unixSocketPath := conf.Conf.Scheme.UnixFile
// 	utils.Log.Infof("Starting Unix socket server on %s", unixSocketPath)
//
// 	// Remove existing socket file if it exists
// 	if utils.Exists(unixSocketPath) {
// 		if err := os.Remove(unixSocketPath); err != nil {
// 			utils.Log.Warnf("Failed to remove existing Unix socket file: %v", err)
// 		}
// 	}
//
// 	unixServer := &http.Server{
// 		Handler:      handler,
// 		ReadTimeout:  DefaultHTTPTimeout,
// 		WriteTimeout: DefaultHTTPTimeout,
// 		IdleTimeout:  DefaultIdleTimeout,
// 	}
//
// 	go func() {
// 		// Create Unix socket listener
// 		listener, err := net.Listen("unix", unixSocketPath)
// 		if err != nil {
// 			utils.Log.Fatalf("Failed to create Unix socket listener: %v", err)
// 			return
// 		}
// 		defer listener.Close()
//
// 		// Set socket file permissions before serving
// 		if err := setUnixSocketPermissions(unixSocketPath); err != nil {
// 			utils.Log.Errorf("Failed to set Unix socket permissions: %v", err)
// 		}
//
// 		// Start serving on the Unix socket
// 		if err := unixServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
// 			utils.Log.Fatalf("Failed to start Unix socket server: %v", err)
// 		}
// 	}()
//
// 	return unixServer
// }
//
// // setUnixSocketPermissions sets the permissions for Unix socket file
// func setUnixSocketPermissions(socketPath string) error {
// 	if conf.Conf.Scheme.UnixFilePerm == "" {
// 		return nil
// 	}
//
// 	mode, err := strconv.ParseUint(conf.Conf.Scheme.UnixFilePerm, 8, 32)
// 	if err != nil {
// 		return errors.Errorf("failed to parse Unix socket file permission '%s': %w", conf.Conf.Scheme.UnixFilePerm, err)
// 	}
//
// 	if err := os.Chmod(socketPath, os.FileMode(mode)); err != nil {
// 		return errors.Errorf("failed to set Unix socket file permissions: %w", err)
// 	}
//
// 	utils.Log.Debugf("Set Unix socket file permissions to %s", conf.Conf.Scheme.UnixFilePerm)
// 	return nil
// }
//
// // startS3Server starts the S3-compatible API server if enabled
// func startS3Server() *http.Server {
// 	if conf.Conf.S3.Port == -1 || !conf.Conf.S3.Enable {
// 		return nil
// 	}
//
// 	// Create a new GoFrame server for S3 API
// 	s3GfServer := g.Server("s3")
// 	s3GfServer.Use(ghttp.MiddlewareHandlerResponse)
// 	// server.InitS3(s3GfServer)
//
// 	s3Addr := fmt.Sprintf("%s:%d", conf.Conf.Scheme.Address, conf.Conf.S3.Port)
// 	utils.Log.Infof("Starting S3-compatible API server on %s (SSL: %v)", s3Addr, conf.Conf.S3.SSL)
//
// 	s3HttpServer := &http.Server{
// 		Addr:         s3Addr,
// 		Handler:      s3GfServer,
// 		ReadTimeout:  S3OperationTimeout,
// 		WriteTimeout: S3OperationTimeout,
// 		IdleTimeout:  DefaultIdleTimeout,
// 	}
//
// 	go func() {
// 		var err error
// 		if conf.Conf.S3.SSL {
// 			if !utils.Exists(conf.Conf.Scheme.CertFile) || !utils.Exists(conf.Conf.Scheme.KeyFile) {
// 				utils.Log.Errorf("Certificate file or key file not found for S3 SSL: %s, %s",
// 					conf.Conf.Scheme.CertFile, conf.Conf.Scheme.KeyFile)
// 				utils.Log.Warn("S3 server will start without SSL despite configuration")
// 				err = s3HttpServer.ListenAndServe()
// 			} else {
// 				err = s3HttpServer.ListenAndServeTLS(conf.Conf.Scheme.CertFile, conf.Conf.Scheme.KeyFile)
// 			}
// 		} else {
// 			err = s3HttpServer.ListenAndServe()
// 		}
//
// 		if err != nil && !errors.Is(err, http.ErrServerClosed) {
// 			utils.Log.Fatalf("Failed to start S3-compatible API server: %v", err)
// 		}
// 	}()
//
// 	return s3HttpServer
// }
//
// // startFTPServer starts the FTP server if enabled
// func startFTPServer() (*ftpserver.FtpServer, *server.FtpMainDriver) {
// 	if conf.Conf.FTP.Listen == "" || !conf.Conf.FTP.Enable {
// 		return nil, nil
// 	}
//
// 	utils.Log.Info("Initializing FTP server...")
// 	ftpDriver, err := server.NewMainDriver()
// 	if err != nil {
// 		utils.Log.Fatalf("Failed to initialize FTP driver: %v", err)
// 		return nil, nil
// 	}
//
// 	utils.Log.Infof("Starting FTP server on %s", conf.Conf.FTP.Listen)
// 	ftpServer := ftpserver.NewFtpServer(ftpDriver)
// 	go func() {
// 		if err := ftpServer.ListenAndServe(); err != nil {
// 			utils.Log.Fatalf("FTP server error: %v", err)
// 		}
// 	}()
//
// 	return ftpServer, ftpDriver
// }
//
// // startSFTPServer starts the SFTP server if enabled
// func startSFTPServer() (*sftpd.SftpServer, *server.SftpDriver) {
// 	if conf.Conf.SFTP.Listen == "" || !conf.Conf.SFTP.Enable {
// 		return nil, nil
// 	}
//
// 	utils.Log.Info("Initializing SFTP server...")
// 	sftpDriver, err := server.NewSftpDriver()
// 	if err != nil {
// 		utils.Log.Fatalf("Failed to initialize SFTP driver: %v", err)
// 		return nil, nil
// 	}
//
// 	utils.Log.Infof("Starting SFTP server on %s", conf.Conf.SFTP.Listen)
// 	sftpServer := sftpd.NewSftpServer(sftpDriver)
// 	go func() {
// 		if err := sftpServer.RunServer(); err != nil {
// 			utils.Log.Fatalf("SFTP server error: %v", err)
// 		}
// 	}()
//
// 	return sftpServer, sftpDriver
// }
//
// // setupGracefulShutdown sets up graceful shutdown for all servers
// func setupGracefulShutdown(serverManager *ServerManager) {
// 	quit := make(chan os.Signal, 1)
// 	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
//
// 	<-quit
// 	utils.Log.Info("Shutdown signal received, gracefully shutting down servers...")
//
// 	// Clean up tasks and release resources
// 	fs.ArchiveContentUploadTaskManager.RemoveAll()
// 	Release()
//
// 	// Create context with timeout for graceful shutdown
// 	ctx, cancel := context.WithTimeout(context.Background(), GracefulShutdownTimeout)
// 	defer cancel()
//
// 	var wg sync.WaitGroup
// 	shutdownHTTPServers(ctx, &wg, serverManager)
// 	shutdownProtocolServers(ctx, &wg, serverManager)
//
// 	wg.Wait()
// 	utils.Log.Info("All servers successfully shut down")
// }
//
// // shutdownHTTPServers shuts down HTTP-based servers
// func shutdownHTTPServers(ctx context.Context, wg *sync.WaitGroup, sm *ServerManager) {
// 	servers := []struct {
// 		name   string
// 		server *http.Server
// 	}{
// 		{name: "HTTP", server: sm.httpServer},
// 		{name: "HTTPS", server: sm.httpsServer},
// 		{name: "Unix socket", server: sm.unixServer},
// 		{name: "S3", server: sm.s3Server},
// 	}
//
// 	for _, s := range servers {
// 		if s.server != nil {
// 			wg.Add(1)
// 			go func(name string, server *http.Server) {
// 				defer wg.Done()
// 				utils.Log.Debugf("Shutting down %s server...", name)
// 				if err := server.Shutdown(ctx); err != nil {
// 					utils.Log.Errorf("%s server shutdown error: %v", name, err)
// 				}
// 			}(s.name, s.server)
// 		}
// 	}
// }
//
// // shutdownProtocolServers shuts down FTP and SFTP servers
// func shutdownProtocolServers(ctx context.Context, wg *sync.WaitGroup, sm *ServerManager) {
// 	if sm.ftpServer != nil && sm.ftpDriver != nil {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			utils.Log.Debug("Shutting down FTP server...")
// 			sm.ftpDriver.Stop()
// 			if err := sm.ftpServer.Stop(); err != nil {
// 				utils.Log.Errorf("FTP server shutdown error: %v", err)
// 			}
// 		}()
// 	}
//
// 	if sm.sftpServer != nil {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			utils.Log.Debug("Shutting down SFTP server...")
// 			if err := sm.sftpServer.Close(); err != nil {
// 				utils.Log.Errorf("SFTP server shutdown error: %v", err)
// 			}
// 		}()
// 	}
// }