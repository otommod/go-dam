package hls

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEncryption(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, `
			#EXTM3U
			#EXT-X-VERSION:6
			#EXT-X-TARGETDURATION:10
			#EXT-X-KEY:METHOD=AES-128,URI="key"
			#EXTINF:9.0,
			seg.ts
		`)
	})

	h := Client{
		Client: srv.Client(),
	}

	err := h.Download(context.Background(), srv.URL+"/media.m3u8", ioutil.Discard)
	// TODO: do errors differently
	if err.Error() == "EXT-X-KEY not supported" {
	} else if err != nil {
		t.Fatal(err)
	}
}
