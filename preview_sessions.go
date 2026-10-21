package main

import (
	"net/http"
	"time"
)

const previewLeaseDuration = 20 * time.Second

// Each open logo editor renews its own lease. Losing one browser must not
// interrupt another editor or leave a preview stream running indefinitely.
func (a *App) handlePreviewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		ClientID string `json:"client_id"`
		Active   bool   `json:"active"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.ClientID == "" || len(request.ClientID) > 128 {
		http.Error(w, "invalid preview client", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	if a.previewClients == nil {
		a.previewClients = make(map[string]time.Time)
	}
	if request.Active {
		a.previewClients[request.ClientID] = time.Now().Add(previewLeaseDuration)
	} else {
		delete(a.previewClients, request.ClientID)
	}
	a.expirePreviewClientsLocked(time.Now())
	var err error
	if request.Active && a.active.URL != "" && !a.status.Running && a.cancel == nil {
		// This endpoint only enables preview decoding for unconfigured channels.
		// Starting/stopping actual detection remains an independent action.
		var refs []ReferenceEntry
		refs, err = a.referencesFor(a.active.URL)
		if err == nil && len(refs) == 0 {
			err = a.startMonitorLocked()
		}
	}
	a.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// Caller holds at least a.mu.RLock.
func (a *App) hasPreviewClientsLocked(now time.Time) bool {
	for _, until := range a.previewClients {
		if until.After(now) {
			return true
		}
	}
	return false
}

// Caller holds a.mu. Timer callbacks re-check deadlines under the same lock,
// so a late expiry cannot cancel a lease that has just been renewed.
func (a *App) expirePreviewClientsLocked(now time.Time) {
	if a.previewTimer != nil {
		a.previewTimer.Stop()
		a.previewTimer = nil
	}
	var next time.Time
	for id, until := range a.previewClients {
		if !until.After(now) {
			delete(a.previewClients, id)
		} else if next.IsZero() || until.Before(next) {
			next = until
		}
	}
	if next.IsZero() {
		if !a.status.Running && a.cancel != nil {
			a.cancel()
			a.cancel = nil
			a.lastFrame = Frame{}
		}
		return
	}
	a.previewTimer = time.AfterFunc(time.Until(next), func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.expirePreviewClientsLocked(time.Now())
	})
}
