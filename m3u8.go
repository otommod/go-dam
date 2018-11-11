package godam

import (
	"io"
	"strings"
	"time"

	"github.com/grafov/m3u8"
)

type MediaPlaylist struct {
	TargetDuration time.Duration
	*m3u8.MediaPlaylist
}

func DecodeFrom(r io.Reader) (playlist m3u8.Playlist, playlistType m3u8.ListType, err error) {
	playlist, playlistType, err = m3u8.DecodeFrom(r, true)

	switch playlistType {
	case m3u8.MASTER:
		// master := playlist.(*m3u8.MasterPlaylist)

	case m3u8.MEDIA:
		media := playlist.(*m3u8.MediaPlaylist)
		media.Segments = media.Segments[:media.Count()]

		var key *m3u8.Key
		for i, seg := range media.Segments {
			seg.SeqId = media.SeqNo + uint64(i)

			if seg.Key != nil {
				if strings.ToUpper(seg.Key.Method) == "NONE" {
					key = nil
				} else {
					key = seg.Key
				}
			}
			seg.Key = key
		}

		playlist = &MediaPlaylist{
			TargetDuration: time.Duration(media.TargetDuration * 1e9),
			MediaPlaylist:  media,
		}
	}

	return
}
