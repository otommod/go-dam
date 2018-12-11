package godam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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
	Client        *http.Client
	SelectVariant func([]*m3u8.Variant) *m3u8.Variant
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

func (h HLS) getVariant(u *url.URL) (*m3u8.Variant, error) {
	r, err := h.Client.Get(u.String())
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
	if h.SelectVariant != nil {
		v := h.SelectVariant(master.Variants)
		if v != nil {
			return v, nil
		}
	} else {
		for _, v := range master.Variants {
			return v, nil
		}
	}
	return nil, errors.New("no streams found")
}

func (h HLS) decodeFrom(ctx context.Context, u *url.URL) (*MediaPlaylist, error) {
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	// add a resonable timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	r, err := h.Client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	} else if r.StatusCode != 200 {
		return nil, HTTPError{r}
	}

	playlist, playlistType, err := DecodeFrom(r.Body)
	if err != nil {
		return nil, err
	} else if playlistType != m3u8.MEDIA {
		return nil, errors.New("expected Media Playlist")
	}

	return playlist.(*MediaPlaylist), nil
}

func (h HLS) Download(ctx context.Context, masterURL *url.URL, dst io.Writer) error {
	variant, err := h.getVariant(masterURL)
	if err != nil {
		return err
	}

	variantURL, err := masterURL.Parse(variant.URI)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	segDataCh := make(chan io.ReadCloser)
	g.Go(func() error {
		defer close(segDataCh)

		var seenMediaSequence uint64
		byterangeOffsets := make(map[string]int64)

		for {
			log.Println("downloading playlist", variant.URI)

			lastLoadedPlaylist := time.Now()
			media, err := h.decodeFrom(ctx, variantURL)
			if err != nil {
				return err
			}

			if media.TargetDuration <= 0 {
				return errors.New("EXT-X-TARGETDURATION was not positive")
			} else if media.TargetDuration >= 90*time.Second {
				return errors.New("EXT-X-TARGETDURATION was too long")
			}

			if media.Iframe {
				return errors.New("EXT-I-FRAMES-ONLY")
			}

			lastSegment := media.Segments[len(media.Segments)-1]
			if seenMediaSequence >= lastSegment.SeqId {
				// If the client reloads a Playlist file and finds that it has not
				// changed, then it MUST wait for a period of one-half the target
				// duration before retrying.

				sleep(ctx, media.TargetDuration/2)
				continue
			}

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

				ctx, cancel := context.WithTimeout(ctx, 2*media.TargetDuration)
				segData, err := h.Client.Do(req.WithContext(ctx))
				if err != nil {
					cancel()
					return err
				}

				segBody := readCloserWithCancel{segData.Body, cancel}
				if segData.StatusCode != 200 {
					segBody.Close()
					return HTTPError{segData}
				}

				select {
				case segDataCh <- segBody:

				case <-ctx.Done():
					segBody.Close()
					return ctx.Err()
				}
			}

			if media.Closed {
				return nil
			}

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
