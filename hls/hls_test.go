package hls

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMediaSequence(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	accessed := make(map[string]int)
	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, `
			#EXTM3U
			#EXT-X-VERSION:3
			#EXT-X-TARGETDURATION:4
			#EXTINF:3.14,
			seg/1.ts
			#EXTINF:3.14,
			seg/2.ts
		`)

		if accessed[r.URL.EscapedPath()] >= 2 {
			io.WriteString(w, "#EXT-X-ENDLIST")
		}
		accessed[r.URL.EscapedPath()]++
	})

	mux.HandleFunc("/seg/", func(w http.ResponseWriter, r *http.Request) {
		if accessed[r.URL.EscapedPath()] > 1 {
			t.Error("segment was not skipped like it should have", r.URL.EscapedPath())
		}

		w.WriteHeader(200)
		w.Write(make([]byte, 1000))
		accessed[r.URL.EscapedPath()]++
	})

	h := Client{
		Client: srv.Client(),
	}

	err := h.Download(context.Background(), srv.URL+"/media.m3u8", ioutil.Discard)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEncryption(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, `
			#EXTM3U
			#EXT-X-VERSION:6
			#EXT-X-TARGETDURATION:4
			#EXT-X-KEY:METHOD=AES-128,URI="key"
			#EXTINF:3.14,
			seg.ts
		`)
	})

	h := Client{
		Client: srv.Client(),
	}

	err := h.Download(context.Background(), srv.URL+"/media.m3u8", ioutil.Discard)
	if err.Error() == "EXT-X-KEY not supported" {
		// TODO: add ErrNotSupported
	} else if err != nil {
		t.Fatal(err)
	}
}

func TestByterange(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, `
			#EXTM3U
			#EXT-X-VERSION:4
			#EXT-X-TARGETDURATION:4
			#EXT-X-BYTERANGE:1000@0
			#EXTINF:3.14,
			video.ts
			#EXTINF:3.14,
			#EXT-X-BYTERANGE:1000
			video.ts
			#EXT-X-ENDLIST
		`)
	})

	var mediaSequence uint64
	mux.HandleFunc("/video.ts", func(w http.ResponseWriter, r *http.Request) {
		httpRange := r.Header.Get("Range")
		switch {
		case mediaSequence == 0 && httpRange == "bytes=0-999":
		case mediaSequence == 1 && httpRange == "bytes=1000-1999":
		default:
			t.Error("requested wrong range", httpRange)
		}

		w.WriteHeader(206)
		w.Write(make([]byte, 1000))
		mediaSequence++ // FIXME: this is not thread safe
	})

	h := Client{
		Client: srv.Client(),
	}

	err := h.Download(context.Background(), srv.URL+"/media.m3u8", ioutil.Discard)
	if err != nil {
		t.Fatal(err)
	}
}
