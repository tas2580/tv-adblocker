package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPlaylistChannelLogos(t *testing.T) {
	playlist := `#EXTM3U
#EXTINF:-1 tvg-logo="https://images.example/logo,a.png" group-title="TV, HD",Sender, HD
rtsp://tv/one
#EXTINF:-1 tvg-logo='../logos/two.png',Two
two.m3u8
#EXTINF:-1 tvg-logo=/logos/three.svg,Three
three.m3u8
#EXTINF:-1 tvg-logo="",Empty
four.m3u8
#EXTINF:-1,No logo
five.m3u8
#EXTINF:-1 tvg-logo="file:///tmp/logo.png",Unsupported
six.m3u8
#EXTINF:-1 group-title="tvg-logo='fake.png'",Quoted attribute
seven.m3u8
#EXTINF:-1 tvg-logo="//cdn.example/eight.png",Protocol relative
eight.m3u8
bare.m3u8
`
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(playlist))}, nil
	})}
	channels, err := fetchPlaylist(client, "https://tv.example/lists/channels.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://images.example/logo,a.png", "https://tv.example/logos/two.png", "https://tv.example/logos/three.svg", "", "", "", "", "https://cdn.example/eight.png", ""}
	if len(channels) != len(want) {
		t.Fatalf("got %d channels, want %d", len(channels), len(want))
	}
	for i, channel := range channels {
		if channel.LogoURL != want[i] {
			t.Errorf("channel %d logo = %q, want %q", i, channel.LogoURL, want[i])
		}
	}
	if channels[0].Name != "Sender, HD" || channels[1].URL != "https://tv.example/lists/two.m3u8" || channels[8].Name != "" {
		t.Fatalf("incorrect channel metadata: %#v", channels)
	}
}

func TestPlaylistRefreshUpdatesActiveLogoDuringPlaceholder(t *testing.T) {
	logo := "https://images.example/new.png"
	started := time.Now().Add(-time.Minute)
	a := &App{
		cfg:             Config{M3UURL: "http://tv.example/list.m3u8"},
		active:          Channel{Name: "ZDF", URL: "rtsp://tv/zdf", LogoURL: "https://images.example/old.png", KodiChannelID: kodiTestID(42)},
		status:          Status{Mode: "placeholder"},
		advertStartedAt: started,
	}
	a.httpClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := "#EXTM3U\n#EXTINF:-1 tvg-logo=\"" + logo + "\",ZDF\nrtsp://tv/zdf\n"
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	for _, next := range []string{logo, ""} {
		logo = next
		if err := a.refreshPlaylist(); err != nil {
			t.Fatal(err)
		}
		if a.active.LogoURL != next || *a.active.KodiChannelID != 42 || a.status.Mode != "placeholder" || !a.advertStartedAt.Equal(started) {
			t.Fatalf("logo refresh changed playback state: %#v", a.active)
		}
	}
	channel := a.active
	channel.LogoURL = "https://images.example/poll.png"
	a.setActiveChannel(channel)
	if a.active.LogoURL != channel.LogoURL || !a.advertStartedAt.Equal(started) {
		t.Fatal("same-channel polling did not update logo without resetting timing")
	}
}
