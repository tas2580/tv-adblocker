package main

import (
	"encoding/json"
	"errors"
	"strings"
)

// IDs are optional because zero is a valid Kodi library ID.
type KodiChannel struct {
	ID    *int   `json:"channelid"`
	Label string `json:"label"`
}

type KodiItem struct {
	ID    *int   `json:"id"`
	Type  string `json:"type"`
	File  string `json:"file"`
	Label string `json:"label"`
}

func (item KodiItem) channelID() *int {
	if item.Type == "channel" && item.ID != nil && *item.ID >= 0 {
		return item.ID
	}
	return nil
}

type kodiRPCError struct{ Message string }

func (e *kodiRPCError) Error() string { return "Kodi RPC error: " + e.Message }

func (a *App) refreshKodiChannels() error {
	a.mu.RLock()
	cfg := a.cfg.Kodi
	a.mu.RUnlock()
	result, _, err := a.kodiCallWithConfig(cfg, "PVR.GetChannels", map[string]any{"channelgroupid": "alltv"})
	if err != nil {
		return err
	}
	var response struct {
		Channels []KodiChannel `json:"channels"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return err
	}
	if response.Channels == nil {
		return errors.New("Kodi returned no TV channel list")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// An in-flight request must not restore IDs from a previous Kodi server.
	if a.cfg.Kodi == cfg {
		a.kodiChannels = response.Channels
	}
	return nil
}

func uniqueKodiChannelID(channels []KodiChannel, name string) *int {
	var id *int
	for _, channel := range channels {
		if channel.ID == nil || *channel.ID < 0 || !sameChannelName(channel.Label, name) {
			continue
		}
		if id != nil && *id != *channel.ID {
			return nil
		}
		id = channel.ID
	}
	return id
}

func channelForKodiItem(channels []Channel, kodiChannels []KodiChannel, item KodiItem) (Channel, bool) {
	label := item.Label
	id := item.channelID()
	if id != nil {
		for _, channel := range kodiChannels {
			if channel.ID != nil && *channel.ID == *id {
				label = channel.Label
				break
			}
		}
	}
	channel, ok := channelForKodiPlayback(channels, item.File, label)
	if !ok {
		return Channel{}, false
	}
	if id == nil {
		id = uniqueKodiChannelID(kodiChannels, channel.Name)
	}
	channel.KodiChannelID = id
	return channel, true
}

// Programme information belongs to the monitored sender, including while
// Kodi is showing our placeholder. Missing EPG data is not a playback error.
func (a *App) syncKodiProgramme(cfg KodiConfig) {
	a.mu.RLock()
	channel := a.active
	id := channel.KodiChannelID
	if id == nil {
		id = uniqueKodiChannelID(a.kodiChannels, channel.Name)
	}
	connectionChanged := a.cfg.Kodi != cfg
	a.mu.RUnlock()
	if connectionChanged {
		return
	}
	title := ""
	if channel.URL != "" && id != nil {
		result, _, err := a.kodiCallWithConfig(cfg, "PVR.GetChannelDetails", map[string]any{
			"channelid": *id, "properties": []string{"broadcastnow"},
		})
		if err == nil {
			var response struct {
				ChannelDetails struct {
					BroadcastNow struct {
						Title string `json:"title"`
						Label string `json:"label"`
					} `json:"broadcastnow"`
				} `json:"channeldetails"`
			}
			if json.Unmarshal(result, &response) == nil {
				title = strings.TrimSpace(response.ChannelDetails.BroadcastNow.Title)
				if title == "" {
					title = strings.TrimSpace(response.ChannelDetails.BroadcastNow.Label)
				}
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Kodi == cfg && sameActiveChannel(a.active, channel) && a.active.URL == channel.URL {
		a.status.KodiProgramme = title
	}
}
