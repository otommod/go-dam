package godam

import (
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/grafov/m3u8"
)

type MediaSegment struct {
	URI *url.URL

	*m3u8.MediaSegment
}

type MediaPlaylist struct {
	TargetDuration time.Duration
	// Segments       []*MediaSegment

	*m3u8.MediaPlaylist
}

func DecodeFrom(r io.Reader, strict bool) (playlist m3u8.Playlist, playlistType m3u8.ListType, err error) {
	playlist, playlistType, err = m3u8.DecodeFrom(r, strict)

	if playlistType == m3u8.MEDIA {
		media := playlist.(*m3u8.MediaPlaylist)

		var key *m3u8.Key
		for i := uint(0); i < media.Count(); i++ {
			seg := media.Segments[i]

			if seg.Key != nil {
				if strings.ToUpper(seg.Key.Method) != "NONE" {
					key = seg.Key
				} else {
					key = nil
				}
			}
			seg.Key = key
		}
		media.Segments = media.Segments[:media.Count()]

		playlist = MediaPlaylist{
			TargetDuration: time.Duration(media.TargetDuration * 1000000000),
			MediaPlaylist:  media,
		}
	}

	return
}
