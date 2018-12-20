package hls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/grafov/m3u8"
	"github.com/otommod/go-dam"
	"golang.org/x/sync/errgroup"
)

type Client struct {
	Client *http.Client
}

func sleep(ctx context.Context, d time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, d)
	<-ctx.Done()
	cancel()
	return
}

type readCloserWithCancel struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (w readCloserWithCancel) Close() error {
	defer w.cancel()
	return w.ReadCloser.Close()
}

func (h Client) ListVariants(uri string) ([]*m3u8.Variant, error) {
	r, err := h.Client.Get(uri)
	if err != nil {
		return nil, err
	} else if r.StatusCode != 200 {
		return nil, dam.HTTPError{r}
	}

	playlist, playlistType, err := parseM3U8(r.Body, uri)
	if err != nil {
		return nil, err
	} else if playlistType != m3u8.MASTER {
		return nil, errors.New("expected Master Playlist")
	}
	master := playlist.(*m3u8.MasterPlaylist)

	return master.Variants, nil
}

func (h Client) readPlaylist(ctx context.Context, uri string) (*MediaPlaylist, error) {
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, err
	}

	// add a resonable timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

	r, err := h.Client.Do(req.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}

	r.Body = readCloserWithCancel{r.Body, cancel}
	if r.StatusCode != 200 {
		return nil, dam.HTTPError{r}
	}
	defer r.Body.Close()

	playlist, playlistType, err := parseM3U8(r.Body, uri)
	if err != nil {
		return nil, err
	} else if playlistType != m3u8.MEDIA {
		return nil, errors.New("expected Media Playlist")
	}

	return playlist.(*MediaPlaylist), nil
}

func (h Client) Download(ctx context.Context, uri string, dst io.Writer) error {
	g, ctx := errgroup.WithContext(ctx)

	segDataCh := make(chan io.ReadCloser)
	g.Go(func() error {
		defer close(segDataCh)

		var seenMediaSequence uint64
		byterangeOffsets := make(map[string]int64)

		for {
			log.Println("[DEBUG] downloading playlist", uri)

			lastLoadedPlaylist := time.Now()
			media, err := h.readPlaylist(ctx, uri)
			if err != nil {
				return err
			}

			if media.Iframe {
				return errors.New("EXT-I-FRAMES-ONLY not supported")
			}

			if media.TargetDuration <= 0 {
				return errors.New("EXT-X-TARGETDURATION non-positive")
			} else if media.TargetDuration >= 90*time.Second {
				return errors.New("EXT-X-TARGETDURATION too long")
			}

			lastSegment := media.Segments[len(media.Segments)-1]
			if seenMediaSequence >= lastSegment.SeqId {
				// § 6.3.4
				// If the client reloads a Playlist file and finds that it has not
				// changed, then it MUST wait for a period of one-half the target
				// duration before retrying.

				sleep(ctx, media.TargetDuration/2)
				continue
			}

			for _, seg := range media.Segments {
				if seg.SeqId <= seenMediaSequence {
					log.Println("[DEBUG] skipping segment", seg.URI)
					continue
				} else if seg.SeqId > seenMediaSequence+1 {
					log.Println("[WARN]", seg.SeqId-seenMediaSequence-1, "segments expired")
				}
				seenMediaSequence = seg.SeqId

				log.Println("[DEBUG] downloading segment", seg.URI)
				req, err := http.NewRequest("GET", seg.URI, nil)
				if err != nil {
					return err
				}

				if seg.Key != nil {
					return errors.New("EXT-X-KEY not supported")
				}
				if seg.Map != nil {
					return errors.New("EXT-X-MAP not supported")
				}

				if seg.Limit < 0 {
					return errors.New("EXT-X-BYTERANGE is negative")
				} else if seg.Limit > 0 {
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

				ctx, cancel := context.WithTimeout(ctx, 2*media.TargetDuration)
				segData, err := h.Client.Do(req.WithContext(ctx))
				if err != nil {
					cancel()
					return err
				}

				segData.Body = readCloserWithCancel{segData.Body, cancel}
				if segData.StatusCode != 200 {
					return dam.HTTPError{segData}
				}

				select {
				case segDataCh <- segData.Body:

				case <-ctx.Done():
					segData.Body.Close()
					return ctx.Err()
				}
			}

			if media.Closed {
				return nil
			}

			// § 6.3.4
			// When a client loads a Playlist file for the first time or reloads a
			// Playlist file and finds that it has changed since the last time it
			// was loaded, the client MUST wait for at least the target duration
			// before attempting to reload the Playlist file again, measured from
			// the last time the client began loading the Playlist file.
			sleep(ctx, time.Until(lastLoadedPlaylist.Add(media.TargetDuration)))
		}
	})

	g.Go(func() error {
		for r := range segDataCh {
			// TODO: limit the maximum buffer size
			var buf bytes.Buffer

			if _, err := io.Copy(&buf, r); err != nil {
				r.Close()
				return err
			}
			if _, err := io.Copy(dst, &buf); err != nil {
				r.Close()
				return err
			}
			if err := r.Close(); err != nil {
				return err
			}
		}
		return nil
	})

	return g.Wait()
}
