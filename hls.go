package godam

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/grafov/m3u8"
	"golang.org/x/sync/errgroup"
)

var (
	// Customize the Transport to have larger connection pool
	transport = &http.Transport{
		// MaxIdleConns:        100,
		// MaxIdleConnsPerHost: 8,
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}
	client = &http.Client{
		// Transport: http.DefaultTransport,
		Transport: transport,
	}
)

type HTTPError struct {
	*http.Response
}

func (e HTTPError) Error() string {
	return e.Status
}

func getBestBandwidth(u *url.URL) (*m3u8.Variant, error) {
	r, err := http.Get(u.String())
	if err != nil {
		return nil, err
	} else if r.StatusCode != 200 {
		return nil, HTTPError{r}
	}

	playlist, playlistType, err := m3u8.DecodeFrom(r.Body, false)
	if err != nil {
		return nil, err
	} else if playlistType != m3u8.MASTER {
		return nil, errors.New("expected Master Playlist")
	}
	master := playlist.(*m3u8.MasterPlaylist)

	log.Printf("found %d variations\n", len(master.Variants))
	sort.Slice(master.Variants, func(i, j int) bool {
		return master.Variants[i].Bandwidth > master.Variants[j].Bandwidth
	})

	for _, v := range master.Variants {
		if v.Iframe {
			// not interested in I-Frame Playlists, it seems they're meant to
			// be used to get those little thumbnails when you hover over the
			// seekbar
			continue
		}
		return v, nil
	}
	return nil, errors.New("no streams found")
}

func HLS(u *url.URL, dst io.Writer) error {
	variant, err := getBestBandwidth(u)
	if err != nil {
		return err
	}

	variantURL, err := u.Parse(variant.URI)
	if err != nil {
		return err
	}

	seenMediaSequence := uint64(0)
	byterangeOffsets := make(map[string]int64)

	for {
		log.Println("downloading playlist", variant)
		r, err := http.Get(variantURL.String())
		if err != nil {
			return err
		} else if r.StatusCode != 200 {
			return HTTPError{r}
		}

		playlist, playlistType, err := DecodeFrom(r.Body, false)
		if err != nil {
			return err
		} else if playlistType != m3u8.MEDIA {
			return errors.New("expected Media Playlist")
		}
		media := playlist.(*MediaPlaylist)

		if media.TargetDuration <= 0 {
			return errors.New("EXT-X-TARGETDURATION was not positive")
		} else if media.TargetDuration >= 90*time.Second {
			return errors.New("EXT-X-TARGETDURATION was too long")
		}

		if media.Iframe {
			// as far as I can tell, "normal" Media Playlists that are not
			// given in an EXT-X-I-FRAME-INF should not be EXT-X-I-FRAMES-ONLY
			return errors.New("playlist is EXT-I-FRAMES-ONLY")
		}

		mediaSequence := media.SeqNo
		if seenMediaSequence >= mediaSequence+uint64(len(media.Segments))-1 {
			// If the client reloads a Playlist file and finds that it has not
			// changed, then it MUST wait for a period of one-half the target
			// duration before retrying.

			time.Sleep(media.TargetDuration / 2)
			continue
		}

		// When a client loads a Playlist file for the first time or reloads a
		// Playlist file and finds that it has changed since the last time it
		// was loaded, the client MUST wait for at least the target duration
		// before attempting to reload the Playlist file again, measured from
		// the last time the client began loading the Playlist file.
		waitCh := time.After(media.TargetDuration)

		segCh := make(chan io.ReadCloser)

		var g errgroup.Group
		g.Go(func() error {
			for r := range segCh {
				if _, err := io.Copy(dst, r); err != nil {
					return err
				}
				if err := r.Close(); err != nil {
					return err
				}
			}
			return nil
		})

		for i, seg := range media.Segments {
			seqID := mediaSequence + uint64(i)
			segURL, _ := variantURL.Parse(seg.URI)

			if seqID <= seenMediaSequence {
				log.Println("skipping segment", seg.URI)
				continue
			} else if seqID > seenMediaSequence+1 {
				log.Println("\033[31m", seqID-seenMediaSequence-1, "segments expired", "\033[m")
			}
			seenMediaSequence = seqID

			if seg.Key != nil {
				return errors.New("segment is encrypted")
			}

			log.Println("downloading segment", seg.URI)
			req, err := http.NewRequest("GET", segURL.String(), nil)
			if err != nil {
				return err
			}

			if seg.Limit < 0 {
				return errors.New("EXT-X-BYTERANGE is negative")
			} else if seg.Limit != 0 {
				offset, ok := byterangeOffsets[seg.URI]
				if seg.Offset != 0 {
					offset = seg.Offset
				} else if !ok {
					return errors.New("EXT-X-BYTERANGE offset not given")
				}

				// the Range header is inclusive
				end := offset + seg.Limit - 1
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end))
				byterangeOffsets[seg.URI] = end + 1
			}

			err = retry(media.TargetDuration, func() error {
				segData, err := http.DefaultClient.Do(req)

				// retry the request if it failed due to network or server issues
				if err != nil {
					return err
				} else if segData.StatusCode != 200 {
					segData.Body.Close()
					if segData.StatusCode >= 500 {
						return HTTPError{segData}
					} else if segData.StatusCode >= 400 {
						// client error, the fault is on us
						return stopRetrying{HTTPError{segData}}
					}
				}

				segCh <- ActivityReadCloser{segData.Body}
				return nil
			})
			if err != nil {
				return err
			}
		}

		close(segCh)
		if err := g.Wait(); err != nil {
			return err
		}

		if media.Closed {
			break
		}
		<-waitCh
	}

	return nil
}
