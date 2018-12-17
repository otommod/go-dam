package hls

import (
	"strings"
	"testing"
	"time"

	"github.com/grafov/m3u8"
)

func compareUnsortedStrings(expected, actual []string) (more, less []string) {
	for _, e := range expected {
		var found bool
		for _, a := range actual {
			if e == a {
				found = true
				break
			}
		}
		if !found {
			less = append(less, e)
		}
	}

	for _, a := range actual {
		var found bool
		for _, e := range expected {
			if e == a {
				found = true
				break
			}
		}
		if !found {
			more = append(more, a)
		}
	}
	return
}

func TestRelativeURIs(t *testing.T) {
	playlist, playlistType, err := parseM3U8(strings.NewReader(`
		#EXTM3U
		#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",URI="audio.m3u8"
		#EXT-X-STREAM-INF:BANDWIDTH=1280000,AUDIO="audio"
		video.m3u8
	`), "http://example.org/master.m3u8")

	if err != nil {
		t.Fatal(err)
	} else if playlistType != m3u8.MASTER {
		t.Fatal("should be Master Playlist")
	}
	master := playlist.(*MasterPlaylist)

	expected := []string{
		"http://example.org/video.m3u8",
		"http://example.org/audio.m3u8",
	}

	actual := []string{
		master.Variants[0].URI,
		master.Variants[0].Alternatives[0].URI,
	}

	for i := range actual {
		if expected[i] != actual[i] {
			t.Error("expected", expected[i], "found", actual[i])
		}
	}

	playlist, playlistType, err = parseM3U8(strings.NewReader(`
		#EXTM3U
		#EXT-X-VERSION:6
		#EXT-X-TARGETDURATION:10
		#EXT-X-KEY:METHOD=AES-128,URI="key"
		#EXT-X-MAP:URI="map"
		#EXTINF:9.0,
		seg.ts
	`), "http://example.org/media.m3u8")

	if err != nil {
		t.Fatal(err)
	} else if playlistType != m3u8.MEDIA {
		t.Fatal("should be Media Playlist")
	}
	media := playlist.(*MediaPlaylist)

	expected = []string{
		"http://example.org/seg.ts",
		"http://example.org/map",
		"http://example.org/key",
	}

	actual = []string{
		media.Segments[0].URI,
		media.Segments[0].Map.URI,
		media.Segments[0].Key.URI,
	}

	for i := range actual {
		if expected[i] != actual[i] {
			t.Error("expected", expected[i], "found", actual[i])
		}
	}
}

func TestAbsoluteURIs(t *testing.T) {
	playlist, playlistType, err := parseM3U8(strings.NewReader(`
		#EXTM3U
		#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",URI="http://example.org/audio.m3u8"
		#EXT-X-STREAM-INF:BANDWIDTH=1280000,AUDIO="audio"
		http://example.org/video.m3u8
	`), "http://example.org/master.m3u8")

	if err != nil {
		t.Fatal(err)
	} else if playlistType != m3u8.MASTER {
		t.Fatal("should be Master Playlist")
	}
	master := playlist.(*MasterPlaylist)

	expected := []string{
		"http://example.org/video.m3u8",
		"http://example.org/audio.m3u8",
	}

	actual := []string{
		master.Variants[0].URI,
		master.Variants[0].Alternatives[0].URI,
	}

	for i := range actual {
		if expected[i] != actual[i] {
			t.Error("expected", expected[i], "found", actual[i])
		}
	}

	playlist, playlistType, err = parseM3U8(strings.NewReader(`
		#EXTM3U
		#EXT-X-VERSION:6
		#EXT-X-TARGETDURATION:10
		#EXT-X-KEY:METHOD=AES-128,URI="http://example.org/key"
		#EXT-X-MAP:URI="http://example.org/map"
		#EXTINF:9.0,
		http://example.org/seg.ts
	`), "http://example.org/media.m3u8")

	if err != nil {
		t.Fatal(err)
	} else if playlistType != m3u8.MEDIA {
		t.Fatal("should be Media Playlist")
	}
	media := playlist.(*MediaPlaylist)

	expected = []string{
		"http://example.org/seg.ts",
		"http://example.org/map",
		"http://example.org/key",
	}

	actual = []string{
		media.Segments[0].URI,
		media.Segments[0].Map.URI,
		media.Segments[0].Key.URI,
	}

	for i := range actual {
		if expected[i] != actual[i] {
			t.Error("expected", expected[i], "found", actual[i])
		}
	}
}

func TestParsingExtraTags(t *testing.T) {
	playlist, playlistType, err := parseM3U8(strings.NewReader(`
		#EXTM3U
		#EXT-X-INDEPENDENT-SEGMENTS
		#EXT-X-START:TIME-OFFSET=1.2,PRECISE=YES
		#EXT-X-STREAM-INF:BANDWIDTH=1280000
		media.m3u8
	`), "")

	if err != nil {
		t.Fatal(err)
	} else if playlistType != m3u8.MASTER {
		t.Fatal("should be Master Playlist")
	}
	master := playlist.(*MasterPlaylist)

	if !master.IndependentSegments {
		t.Error("did not parse EXT-X-INDEPENDENT-SEGMENTS")
	}

	if !master.StartPrecise {
		t.Error("did not parse EXT-X-START:PRECISE")
	}

	if master.StartOffset != 1200*time.Millisecond {
		t.Error("did not parse EXT-X-START:TIME-OFFSET")
	}
}

func TestRenditions(t *testing.T) {
	playlist, playlistType, err := parseM3U8(strings.NewReader(`
		#EXTM3U
		#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aac",NAME="English",LANGUAGE="en",URI="english-audio.m3u8"
		#EXT-X-STREAM-INF:BANDWIDTH=65000,AUDIO="aac"
		english-audio.m3u8
		#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aac",NAME="Deutsch",LANGUAGE="de",URI="german-audio.m3u8"
		#EXT-X-STREAM-INF:BANDWIDTH=1280000,AUDIO="aac"
		video-only.m3u8
		#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aac",NAME="Commentary",LANGUAGE="en",URI="commentary-audio.m3u8"
		#EXT-X-STREAM-INF:BANDWIDTH=2560000,AUDIO="aac"
		mid/video-only.m3u8
	`), "")

	if err != nil {
		t.Fatal(err)
	} else if playlistType != m3u8.MASTER {
		t.Fatal("should be Master Playlist")
	}
	master := playlist.(*MasterPlaylist)

	expected := []string{
		"Commentary",
		"Deutsch",
		"English",
	}

	for _, v := range master.Variants {
		var actual []string
		for _, r := range v.Alternatives {
			actual = append(actual, r.Name)
		}
		more, less := compareUnsortedStrings(expected, actual)
		if len(more) > 0 {
			t.Error("unexpected rendition(s)", more)
		}
		if len(less) > 0 {
			t.Error("expected rendition(s) not found", less)
		}
	}
}
