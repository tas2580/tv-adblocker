package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func kodiTestID(id int) *int { return &id }

func kodiTestClient(t *testing.T, call func(string, map[string]json.RawMessage) (string, error)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		body, err := call(request.Method, request.Params)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

func TestKodiChannelResolution(t *testing.T) {
	channels := []Channel{{Name: "RTL ZWEI", URL: "rtsp://tv/two"}, {Name: "ZDF", URL: "rtsp://tv/zdf"}}
	kodiChannels := []KodiChannel{{ID: kodiTestID(0), Label: "rtl-zwei"}, {ID: kodiTestID(9), Label: "ZDF"}}
	for _, test := range []struct {
		name   string
		item   KodiItem
		list   []KodiChannel
		wantID *int
	}{
		{"programme title", KodiItem{ID: kodiTestID(0), Type: "channel", Label: "Evening programme", File: "pvr://channels/tv/0"}, kodiChannels, kodiTestID(0)},
		{"URL playback", KodiItem{Label: "Unknown", File: "rtsp://tv/two"}, kodiChannels, kodiTestID(0)},
		{"movie ID is not a channel ID", KodiItem{ID: kodiTestID(9), Type: "movie", File: "rtsp://tv/two"}, kodiChannels, kodiTestID(0)},
		{"ambiguous Kodi name", KodiItem{Label: "RTL ZWEI"}, append(append([]KodiChannel{}, kodiChannels...), KodiChannel{ID: kodiTestID(10), Label: "RTL-ZWEI"}), nil},
		{"missing Kodi channel", KodiItem{Label: "RTL ZWEI"}, nil, nil},
		{"observed ID without list", KodiItem{ID: kodiTestID(12), Type: "channel", Label: "RTL ZWEI"}, nil, kodiTestID(12)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := channelForKodiItem(channels, test.list, test.item)
			if !ok || got.Name != "RTL ZWEI" || !reflect.DeepEqual(got.KodiChannelID, test.wantID) {
				t.Fatalf("resolved = %#v, ok = %v", got, ok)
			}
		})
	}
	duplicate := append(channels, Channel{Name: "rtl-zwei", URL: "rtsp://tv/other"})
	if _, ok := channelForKodiPlayback(duplicate, "", "RTL ZWEI"); ok {
		t.Fatal("ambiguous M3U name was accepted")
	}
	if got, ok := channelForKodiPlayback(duplicate, "rtsp://tv/two", "RTL ZWEI"); !ok || got.URL != "rtsp://tv/two" {
		t.Fatal("unique stream URL did not disambiguate M3U entries")
	}
}

func TestKodiChannelListRefresh(t *testing.T) {
	for _, test := range []struct {
		name, reply        string
		changed, wantError bool
	}{
		{"success", `{"result":{"channels":[{"channelid":42,"label":"ZDF"}]}}`, false, false},
		{"empty list", `{"result":{"channels":[]}}`, false, false},
		{"RPC failure", `{"error":{"code":-1,"message":"PVR unavailable"}}`, false, true},
		{"invalid response", `{"result":{}}`, false, true},
		{"connection changed", `{"result":{"channels":[{"channelid":42,"label":"ZDF"}]}}`, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &App{cfg: Config{Kodi: KodiConfig{URL: "http://kodi.test"}}}
			a.httpClient = kodiTestClient(t, func(method string, params map[string]json.RawMessage) (string, error) {
				if method != "PVR.GetChannels" || string(params["channelgroupid"]) != `"alltv"` {
					t.Fatalf("unexpected request: %s %s", method, params)
				}
				if test.changed {
					a.mu.Lock()
					a.cfg.Kodi.URL = "http://other-kodi.test"
					a.mu.Unlock()
				}
				return test.reply, nil
			})
			if err := a.refreshKodiChannels(); (err != nil) != test.wantError {
				t.Fatalf("refresh error = %v", err)
			}
			if test.name == "success" {
				if len(a.kodiChannels) != 1 || *a.kodiChannels[0].ID != 42 {
					t.Fatal("channel list not loaded")
				}
			} else if len(a.kodiChannels) != 0 {
				t.Fatal("unexpected cached channels")
			}
		})
	}
}

func TestKodiPlaceholderRoundTrip(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "automatic"}[automatic], func(t *testing.T) {
			cfg := Config{Kodi: KodiConfig{URL: "http://kodi.test"}, PlaceholderSource: "kodi-local", KodiPlaceholder: "file:///advert.mp4", ProgramCooldownSeconds: 600}
			channel := Channel{Name: "ZDF", URL: "rtsp://tv/zdf"}
			a := &App{cfg: cfg, active: channel, playlist: []Channel{channel}}
			item := `{"id":42,"type":"channel","label":"Evening news","file":"pvr://channels/tv/zdf"}`
			var opened []map[string]any
			a.httpClient = kodiTestClient(t, func(method string, params map[string]json.RawMessage) (string, error) {
				switch method {
				case "PVR.GetChannels":
					return `{"result":{"channels":[{"channelid":42,"label":"ZDF"}]}}`, nil
				case "PVR.GetChannelDetails":
					return `{"result":{"channeldetails":{"broadcastnow":{"title":"Evening news"}}}}`, nil
				case "Player.GetActivePlayers":
					return `{"result":[{"playerid":1,"type":"video"}]}`, nil
				case "Player.GetItem":
					if string(params["properties"]) != `["file"]` {
						t.Fatalf("invalid properties: %s", params["properties"])
					}
					return `{"result":{"item":` + item + `}}`, nil
				case "Playlist.GetPlaylists":
					return `{"result":[]}`, nil
				case "Player.Open":
					var target map[string]any
					if err := json.Unmarshal(params["item"], &target); err != nil {
						t.Fatal(err)
					}
					opened = append(opened, target)
					return `{"result":"OK"}`, nil
				default:
					t.Fatalf("unexpected method: %s", method)
					return "", nil
				}
			})
			if err := a.refreshKodiChannels(); err != nil {
				t.Fatal(err)
			}
			started := time.Now().Add(-time.Minute)
			a.programStartedAt = started
			a.syncKodiPlayback()
			if a.status.KodiProgramme != "Evening news" {
				t.Fatal("programme title was not loaded")
			}
			if a.active.KodiChannelID == nil || *a.active.KodiChannelID != 42 || !a.programStartedAt.Equal(started) {
				t.Fatal("enriching the channel reset timing or lost the ID")
			}
			if err := a.play("placeholder", automatic); err != nil {
				t.Fatal(err)
			}
			item = `{"type":"movie","id":100,"label":"Advertisement","file":"file:///advert.mp4"}`
			a.syncKodiPlayback()
			if a.active.KodiChannelID == nil || *a.active.KodiChannelID != 42 {
				t.Fatal("placeholder lost sender ID")
			}
			if a.status.KodiProgramme != "Evening news" {
				t.Fatal("placeholder lost the sender's programme title")
			}
			if automatic {
				a.observe(true, 1, Config{PresentFrames: 1})
			} else {
				recorder := httptest.NewRecorder()
				a.handlePlaySender(recorder, httptest.NewRequest(http.MethodPost, "/api/play-sender", nil))
				if recorder.Code != http.StatusOK {
					t.Fatal(recorder.Body.String())
				}
			}
			want := []map[string]any{{"file": cfg.KodiPlaceholder}, {"channelid": float64(42)}}
			if !reflect.DeepEqual(opened, want) {
				t.Fatalf("opened = %#v", opened)
			}
			item = `{"id":42,"type":"channel","label":"Evening news","file":"pvr://channels/tv/zdf"}`
			a.syncKodiPlayback()
			if a.active.Name != "ZDF" || a.status.Mode != "sender" || a.programCooldownUntil.After(time.Now()) != automatic {
				t.Fatalf("unexpected return state: %#v", a.status)
			}
		})
	}
}

func TestKodiReturnFallback(t *testing.T) {
	for _, test := range []struct {
		name, reply             string
		id                      *int
		networkError, wantError bool
		opens                   int
	}{
		{"missing ID", `{"result":"OK"}`, nil, false, false, 1},
		{"ID accepted", `{"result":"OK"}`, kodiTestID(42), false, false, 1},
		{"ID rejected", `{"error":{"code":-32602,"message":"Invalid channel"}}`, kodiTestID(42), false, false, 2},
		{"network failure", "", kodiTestID(42), true, true, 1},
		{"invalid response", `broken`, kodiTestID(42), false, true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &App{cfg: Config{Kodi: KodiConfig{URL: "http://kodi.test"}}, active: Channel{Name: "ZDF", URL: "rtsp://tv/zdf", KodiChannelID: test.id}}
			var opened []map[string]any
			a.httpClient = kodiTestClient(t, func(method string, params map[string]json.RawMessage) (string, error) {
				if method == "Playlist.GetPlaylists" {
					return `{"result":[]}`, nil
				}
				if method != "Player.Open" {
					t.Fatalf("unexpected method: %s", method)
				}
				var item map[string]any
				if err := json.Unmarshal(params["item"], &item); err != nil {
					t.Fatal(err)
				}
				opened = append(opened, item)
				if test.networkError {
					return "", io.ErrUnexpectedEOF
				}
				if len(opened) > 1 {
					return `{"result":"OK"}`, nil
				}
				return test.reply, nil
			})
			if err := a.play("sender", false); (err != nil) != test.wantError {
				t.Fatalf("play error = %v", err)
			}
			if len(opened) != test.opens {
				t.Fatalf("opened = %#v", opened)
			}
			if test.id == nil || test.opens == 2 {
				if !reflect.DeepEqual(opened[len(opened)-1], map[string]any{"file": a.active.URL}) {
					t.Fatal("fallback did not open URL")
				}
			}
		})
	}
}

func TestKodiReturnResolvesCachedList(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{false: "unique", true: "ambiguous"}[ambiguous], func(t *testing.T) {
			a := &App{
				cfg:          Config{Kodi: KodiConfig{URL: "http://kodi.test"}},
				active:       Channel{Name: "ZDF", URL: "rtsp://tv/zdf"},
				kodiChannels: []KodiChannel{{ID: kodiTestID(42), Label: "ZDF"}},
			}
			want := map[string]any{"channelid": float64(42)}
			if ambiguous {
				a.kodiChannels = append(a.kodiChannels, KodiChannel{ID: kodiTestID(43), Label: "zdf"})
				want = map[string]any{"file": a.active.URL}
			}
			opened := false
			a.httpClient = kodiTestClient(t, func(method string, params map[string]json.RawMessage) (string, error) {
				if method == "Playlist.GetPlaylists" {
					return `{"result":[]}`, nil
				}
				if method != "Player.Open" {
					t.Fatalf("unexpected method: %s", method)
				}
				var item map[string]any
				if err := json.Unmarshal(params["item"], &item); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(item, want) {
					t.Fatalf("opened = %#v, want %#v", item, want)
				}
				opened = true
				return `{"result":"OK"}`, nil
			})
			if err := a.play("sender", false); err != nil {
				t.Fatal(err)
			}
			if !opened {
				t.Fatal("sender was not opened")
			}
		})
	}
}

func TestSameActiveChannelUsesKodiIdentity(t *testing.T) {
	a := Channel{Name: "ZDF", URL: "rtsp://tv/zdf", KodiChannelID: kodiTestID(42)}
	b := Channel{Name: "ZDF", URL: a.URL, KodiChannelID: kodiTestID(43)}
	if sameActiveChannel(a, b) {
		t.Fatal("different Kodi IDs were treated as the same channel")
	}
	b.Name = "ZDF renamed"
	b.KodiChannelID = kodiTestID(42)
	if !sameActiveChannel(a, b) {
		t.Fatal("same Kodi ID was treated as a channel change")
	}
}

func TestKodiProgrammeIsOptional(t *testing.T) {
	for _, test := range []struct {
		name, reply, want string
		networkError      bool
	}{
		{"title", `{"result":{"channeldetails":{"broadcastnow":{"title":"  Heute journal  ","label":"Other"}}}}`, "Heute journal", false},
		{"broadcast label", `{"result":{"channeldetails":{"broadcastnow":{"label":"Heute"}}}}`, "Heute", false},
		{"no EPG", `{"result":{"channeldetails":{}}}`, "", false},
		{"empty EPG", `{"result":{"channeldetails":{"broadcastnow":{"title":"  "}}}}`, "", false},
		{"null EPG", `{"result":{"channeldetails":{"broadcastnow":null}}}`, "", false},
		{"unsupported", `{"error":{"code":-32601,"message":"Method not found"}}`, "", false},
		{"invalid response", `{"result":{"channeldetails":{"broadcastnow":[]}}}`, "", false},
		{"network error", "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &App{
				cfg:    Config{Kodi: KodiConfig{URL: "http://kodi.test"}},
				active: Channel{Name: "ZDF", URL: "rtsp://tv/zdf", KodiChannelID: kodiTestID(42)},
				status: Status{Mode: "sender", KodiProgramme: "Old programme"},
			}
			a.httpClient = kodiTestClient(t, func(method string, params map[string]json.RawMessage) (string, error) {
				if method != "PVR.GetChannelDetails" || string(params["channelid"]) != "42" || string(params["properties"]) != `["broadcastnow"]` {
					t.Fatalf("unexpected EPG request: %s %s", method, params)
				}
				if test.networkError {
					return "", io.ErrUnexpectedEOF
				}
				return test.reply, nil
			})
			a.syncKodiProgramme(a.cfg.Kodi)
			if a.status.KodiProgramme != test.want || a.status.KodiError != "" || a.status.LastError != "" || a.status.Mode != "sender" {
				t.Fatalf("unexpected programme status: %#v", a.status)
			}
		})
	}
}

func TestKodiProgrammeDiscardsOldSenderOrConnection(t *testing.T) {
	for _, connectionChanged := range []bool{false, true} {
		t.Run(map[bool]string{false: "sender", true: "connection"}[connectionChanged], func(t *testing.T) {
			a := &App{
				cfg:    Config{Kodi: KodiConfig{URL: "http://kodi.test"}},
				active: Channel{Name: "ZDF", URL: "rtsp://tv/zdf", KodiChannelID: kodiTestID(42)},
			}
			a.httpClient = kodiTestClient(t, func(method string, params map[string]json.RawMessage) (string, error) {
				a.mu.Lock()
				if connectionChanged {
					a.cfg.Kodi.URL = "http://other-kodi.test"
				} else {
					a.active = Channel{Name: "Das Erste", URL: "rtsp://tv/one", KodiChannelID: kodiTestID(43)}
				}
				a.status.KodiProgramme = "New programme"
				a.mu.Unlock()
				return `{"result":{"channeldetails":{"broadcastnow":{"title":"Old programme"}}}}`, nil
			})
			a.syncKodiProgramme(a.cfg.Kodi)
			if a.status.KodiProgramme != "New programme" {
				t.Fatal("stale EPG response replaced current programme")
			}
		})
	}
}

func TestKodiProgrammeClearedWithoutSender(t *testing.T) {
	a := &App{
		cfg:    Config{Kodi: KodiConfig{URL: "http://kodi.test"}},
		active: Channel{Name: "ZDF", URL: "rtsp://tv/zdf", KodiChannelID: kodiTestID(42)},
		status: Status{KodiProgramme: "Old programme"},
	}
	a.httpClient = kodiTestClient(t, func(method string, params map[string]json.RawMessage) (string, error) {
		if method != "Player.GetActivePlayers" {
			t.Fatalf("unexpected request without player: %s", method)
		}
		return `{"result":[]}`, nil
	})
	a.syncKodiPlayback()
	if a.status.KodiProgramme != "" {
		t.Fatal("stopped playback retained the programme title")
	}
	a.active = Channel{Name: "Unknown", URL: "rtsp://tv/unknown"}
	a.status.KodiProgramme = "Old programme"
	a.syncKodiProgramme(a.cfg.Kodi)
	if a.status.KodiProgramme != "" {
		t.Fatal("unresolved sender retained the programme title")
	}
}
