package rpc

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// caller is the interface for making RPC calls to aria2 daemon
type caller interface {
	// Call sends a request of rpc to aria2 daemon
	Call(method string, params, reply any) (err error)
	Close() error
}

// httpCaller implements caller interface using HTTP
type httpCaller struct {
	uri    string
	c      *http.Client
	cancel context.CancelFunc
	wg     *sync.WaitGroup
	once   sync.Once
}

// newHTTPCaller creates a new HTTP-based RPC caller
func newHTTPCaller(ctx context.Context, u *url.URL, timeout time.Duration, notifier Notifier) *httpCaller {
	c := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 1,
			MaxConnsPerHost:     1,
			// TLSClientConfig:     tlsConfig,
			Dial: (&net.Dialer{
				Timeout:   timeout,
				KeepAlive: 60 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   3 * time.Second,
			ResponseHeaderTimeout: timeout,
		},
	}
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	h := &httpCaller{uri: u.String(), c: c, cancel: cancel, wg: &wg}
	if notifier != nil {
		h.setNotifier(ctx, *u, notifier)
	}
	return h
}

// Close shuts down the HTTP caller
func (h *httpCaller) Close() (err error) {
	h.once.Do(func() {
		h.cancel()
		h.wg.Wait()
	})
	return
}

// setNotifier establishes a WebSocket connection for notifications
func (h *httpCaller) setNotifier(ctx context.Context, u url.URL, notifier Notifier) error {
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer conn.Close()
		select {
		case <-ctx.Done():
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			if err := conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
				log.WithError(err).Warn("Failed to send websocket close message")
			}
			return
		}
	}()
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		var request websocketResponse
		var err error
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err = conn.ReadJSON(&request); err != nil {
				select {
				case <-ctx.Done():
					return
				default:
				}
				log.WithError(err).Error("Failed to read JSON from websocket")
				return
			}
			processNotification(request, notifier)
		}
	}()
	return nil
}

// Call makes an RPC call using HTTP
func (h *httpCaller) Call(method string, params, reply any) (err error) {
	payload, err := EncodeClientRequest(method, params)
	if err != nil {
		return
	}
	r, err := h.c.Post(h.uri, "application/json", payload)
	if err != nil {
		return
	}
	defer r.Body.Close()
	err = DecodeClientResponse(r.Body, reply)
	return
}

// websocketCaller implements caller interface using WebSocket
type websocketCaller struct {
	conn     *websocket.Conn
	sendChan chan *sendRequest
	cancel   context.CancelFunc
	wg       *sync.WaitGroup
	once     sync.Once
	timeout  time.Duration
}

// newWebsocketCaller creates a new WebSocket-based RPC caller
func newWebsocketCaller(ctx context.Context, uri string, timeout time.Duration, notifier Notifier) (*websocketCaller, error) {
	var header = http.Header{}
	conn, _, err := websocket.DefaultDialer.Dial(uri, header)
	if err != nil {
		return nil, err
	}

	sendChan := make(chan *sendRequest, 16)
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	w := &websocketCaller{conn: conn, wg: &wg, cancel: cancel, sendChan: sendChan, timeout: timeout}
	processor := NewResponseProcessor()
	wg.Add(1)
	go func() { // routine:recv
		defer wg.Done()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			var resp websocketResponse
			if err := conn.ReadJSON(&resp); err != nil {
				select {
				case <-ctx.Done():
					return
				default:
				}
				log.WithError(err).Error("Failed to read JSON from websocket")
				return
			}
			if resp.ID == nil { // RPC notifications
				if notifier != nil {
					processNotification(resp, notifier)
				}
				continue
			}
			processor.Process(resp.clientResponse)
		}
	}()
	wg.Add(1)
	go func() { // routine:send
		defer wg.Done()
		defer cancel()
		defer w.conn.Close()

		for {
			select {
			case <-ctx.Done():
				if err := w.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
					log.WithError(err).Warn("Failed to send websocket close message")
				}
				return
			case req := <-sendChan:
				processor.Add(req.request.ID, func(resp clientResponse) error {
					err := resp.decode(req.reply)
					req.cancel()
					return err
				})
				w.conn.SetWriteDeadline(time.Now().Add(timeout))
				if err := w.conn.WriteJSON(req.request); err != nil {
					log.WithError(err).Error("Failed to write JSON to websocket")
				}
			}
		}
	}()

	return w, nil
}

// Close shuts down the WebSocket caller
func (w *websocketCaller) Close() (err error) {
	w.once.Do(func() {
		w.cancel()
		w.wg.Wait()
	})
	return
}

// Call makes an RPC call using WebSocket
func (w websocketCaller) Call(method string, params, reply any) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), w.timeout)
	defer cancel()
	select {
	case w.sendChan <- &sendRequest{cancel: cancel, request: &clientRequest{
		Version: "2.0",
		Method:  method,
		Params:  params,
		ID:      reqid(),
	}, reply: reply}:
		return
	case <-ctx.Done():
		return errConnTimeout
	}
}

// processNotification handles notification events from aria2
func processNotification(resp websocketResponse, notifier Notifier) {
	switch resp.Method {
	case "aria2.onDownloadStart":
		notifier.OnDownloadStart(resp.Params)
	case "aria2.onDownloadPause":
		notifier.OnDownloadPause(resp.Params)
	case "aria2.onDownloadStop":
		notifier.OnDownloadStop(resp.Params)
	case "aria2.onDownloadComplete":
		notifier.OnDownloadComplete(resp.Params)
	case "aria2.onDownloadError":
		notifier.OnDownloadError(resp.Params)
	case "aria2.onBtDownloadComplete":
		notifier.OnBtDownloadComplete(resp.Params)
	default:
		log.WithField("method", resp.Method).Warn("Unexpected notification method")
	}
}

// sendRequest represents a request to be sent via WebSocket
type sendRequest struct {
	cancel  context.CancelFunc
	request *clientRequest
	reply   any
}

// generateReqID returns a function that generates unique request IDs.
// It is implemented as a closure to ensure that the counter is thread-safe
// and initialized only once.
var reqid = func() func() uint64 {
	var id = uint64(time.Now().UnixNano())
	return func() uint64 {
		return atomic.AddUint64(&id, 1)
	}
}()