package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/grafov/m3u8"
)

func getBestBandwidth(u *url.URL) (*m3u8.Variant, error) {
	r, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}

	playlist, playlistType, err := m3u8.DecodeFrom(r.Body, false)
	if err != nil {
		return nil, err
	} else if playlistType != m3u8.MASTER {
		return nil, errors.New("expected Master Playlist")
	}
	master := playlist.(*m3u8.MasterPlaylist)

	if len(master.Variants) < 1 {
		return nil, errors.New("no streams found")
	} else if len(master.Variants) > 1 {
		log.Printf("found %d variations\n", len(master.Variants))
		sort.Slice(master.Variants, func(i, j int) bool {
			return master.Variants[i].Bandwidth > master.Variants[j].Bandwidth
		})
	}

	for _, v := range master.Variants {
		if v.Iframe {
			// not interested in I-Frame Playlists, it seems they're meant to
			// be used to get those little thumbnails when hover over the
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

	fp := dst
	// fp := ActivityWriter{dst}
	seenMediaSequence := uint64(0)
	byterangeOffsets := make(map[string]int64)

	for {
		log.Println("downloading playlist", variant)
		r, err := http.Get(variantURL.String())
		if err != nil {
			return err
		}

		playlist, playlistType, err := DecodeFrom(r.Body, false)
		if err != nil {
			return err
		} else if playlistType != m3u8.MEDIA {
			return errors.New("expected Media Playlist")
		}
		media := playlist.(MediaPlaylist)

		if media.TargetDuration <= 0 {
			return errors.New("EXT-X-TARGETDURATION was not positive")
		} else if media.TargetDuration >= 90*time.Second {
			return errors.New("EXT-X-TARGETDURATION was too long")
		}

		if media.Iframe {
			// as far as I can tell, "normal" Media Playlists that are not
			// given in an EXT-X-I-FRAME-INF should not be EXT-X-I-FRAMES-ONLY
			log.Fatal("EXT-I-FRAMES-ONLY")
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
		doneCh := make(chan struct{})

		go func() {
			for r := range segCh {
				if _, err := io.Copy(fp, r); err != nil {
					// return err
					log.Fatal(err)
				}
				if err := r.Close(); err != nil {
					// return err
					log.Fatal(err)
				}
			}
			close(doneCh)
		}()

		for i, seg := range media.Segments {
			seqID := mediaSequence + uint64(i)
			segURL, _ := variantURL.Parse(seg.URI)

			if seqID <= seenMediaSequence {
				log.Println("skipping segment", seg.URI)
				continue
			} else if seqID > seenMediaSequence+1 {
				log.Printf("\033[31m%d segments expired\033[m\n", seqID-seenMediaSequence-1)
			}
			seenMediaSequence = seqID

			if seg.Key != nil {
				log.Fatalln("segment is encrypted")
			}

			log.Println("downloading segment", seg.URI)
			req, err := http.NewRequest("GET", segURL.String(), nil)
			if err != nil {
				log.Fatal(err)
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

				end := offset + seg.Limit - 1
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end))
				byterangeOffsets[seg.URI] = end + 1
			}

			segData, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}

			segCh <- ActivityReadCloser{segData.Body}
		}

		close(segCh)
		<-doneCh

		if media.Closed {
			break
		}
		<-waitCh
	}

	return nil
}
