package rpc

import (
	log "github.com/sirupsen/logrus"
)

// Event represents an aria2 download event notification
type Event struct {
	Gid string `json:"gid"` // GID of the download
}

// The RPC server might send notifications to the client.
// Notifications is unidirectional, therefore the client which receives the notification must not respond to it.
// The method signature of a notification is much like a normal method request but lacks the id key

// websocketResponse represents a websocket notification response from aria2
type websocketResponse struct {
	clientResponse
	Method string  `json:"method"`
	Params []Event `json:"params"`
}

// Notifier handles rpc notification from aria2 server
type Notifier interface {
	// OnDownloadStart will be sent when a download is started.
	OnDownloadStart([]Event)
	// OnDownloadPause will be sent when a download is paused.
	OnDownloadPause([]Event)
	// OnDownloadStop will be sent when a download is stopped by the user.
	OnDownloadStop([]Event)
	// OnDownloadComplete will be sent when a download is complete. For BitTorrent downloads, this notification is sent when the download is complete and seeding is over.
	OnDownloadComplete([]Event)
	// OnDownloadError will be sent when a download is stopped due to an error.
	OnDownloadError([]Event)
	// OnBtDownloadComplete will be sent when a torrent download is complete but seeding is still going on.
	OnBtDownloadComplete([]Event)
}

// DummyNotifier implements Notifier interface with simple logging
type DummyNotifier struct{}

// OnDownloadStart logs when downloads start
func (DummyNotifier) OnDownloadStart(events []Event) {
	for _, event := range events {
		log.WithField("gid", event.Gid).Info("Download started")
	}
}

// OnDownloadPause logs when downloads are paused
func (DummyNotifier) OnDownloadPause(events []Event) {
	for _, event := range events {
		log.WithField("gid", event.Gid).Info("Download paused")
	}
}

// OnDownloadStop logs when downloads are stopped
func (DummyNotifier) OnDownloadStop(events []Event) {
	for _, event := range events {
		log.WithField("gid", event.Gid).Info("Download stopped")
	}
}

// OnDownloadComplete logs when downloads complete
func (DummyNotifier) OnDownloadComplete(events []Event) {
	for _, event := range events {
		log.WithField("gid", event.Gid).Info("Download completed")
	}
}

// OnDownloadError logs when downloads error
func (DummyNotifier) OnDownloadError(events []Event) {
	for _, event := range events {
		log.WithField("gid", event.Gid).Error("Download error")
	}
}

// OnBtDownloadComplete logs when BitTorrent downloads complete
func (DummyNotifier) OnBtDownloadComplete(events []Event) {
	for _, event := range events {
		log.WithField("gid", event.Gid).Info("BitTorrent download completed")
	}
}
