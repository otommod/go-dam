package godam

import (
	"context"
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

type HTTPError struct {
	*http.Response
}

func (e HTTPError) Error() string {
	return e.Status
}

type HLS struct {
	Client *http.Client
}

func getBestBandwidth(u *url.URL) (*m3u8.Variant, error) {
	r, err := http.Get(u.String())
	if err != nil {
		return nil, err
	} else if r.StatusCode != 200 {
		return nil, HTTPError{r}
	}

	playlist, playlistType, err := DecodeFrom(r.Body)
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
			// EXT-X-I-FRAME-STREAM-INF
			// Not interested.  They're meant for so-called Trick Play, to get
			// those little thumbnails when you hover over the seekbar.
			continue
		}
		return v, nil
	}
	return nil, errors.New("no streams found")
}

func (h HLS) Get(masterURL *url.URL, dst io.Writer) error {
	variant, err := getBestBandwidth(masterURL)
	if err != nil {
		return err
	}

	variantURL, err := masterURL.Parse(variant.URI)
	if err != nil {
		return err
	}

	seenMediaSequence := uint64(0)
	byterangeOffsets := make(map[string]int64)

	for {
		log.Println("downloading playlist", variant.URI)
		r, err := http.Get(variantURL.String())
		if err != nil {
			return err
		} else if r.StatusCode != 200 {
			return HTTPError{r}
		}

		playlist, playlistType, err := DecodeFrom(r.Body)
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
			// as far as I can tell, non EXT-X-I-FRAME-STREAM-INF Media
			// Playlists should not be EXT-X-I-FRAMES-ONLY
			return errors.New("EXT-I-FRAMES-ONLY")
		}

		lastSegment := media.Segments[len(media.Segments)-1]
		if seenMediaSequence >= lastSegment.SeqId {
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

		for _, seg := range media.Segments {
			if seg.SeqId <= seenMediaSequence {
				log.Println("skipping segment", seg.URI)
				continue
			} else if seg.SeqId > seenMediaSequence+1 {
				log.Println("\033[31m", seg.SeqId-seenMediaSequence-1, "segments expired", "\033[m")
			}
			seenMediaSequence = seg.SeqId

			if seg.Key != nil {
				return errors.New("segment is encrypted")
			}

			segURL, err := variantURL.Parse(seg.URI)
			if err != nil {
				return err
			}

			log.Println("downloading segment", seg.URI)
			req, err := http.NewRequest("GET", segURL.String(), nil)
			if err != nil {
				return err
			}

			if seg.Limit < 0 {
				return errors.New("EXT-X-BYTERANGE is negative")
			} else if seg.Limit != 0 {
				offset, ok := byterangeOffsets[segURL.String()]
				if seg.Offset != 0 {
					offset = seg.Offset
				} else if !ok {
					return errors.New("EXT-X-BYTERANGE offset not given")
				}

				// the Range header is inclusive
				end := offset + seg.Limit - 1
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end))
				byterangeOffsets[segURL.String()] = end + 1
			}

			err = retry(media.TargetDuration, func() error {
				segData, err := h.Client.Do(req)

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

				segCh <- segData.Body
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
