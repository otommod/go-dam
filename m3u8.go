package godam

import (
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/grafov/m3u8"
)

type MediaPlaylist struct {
	TargetDuration time.Duration
	*m3u8.MediaPlaylist
}

type renditionGroupKey struct {
	Type, GroupID string
}

func DecodeFrom(r io.Reader, playlistURI string) (playlist m3u8.Playlist, playlistType m3u8.ListType, err error) {
	playlist, playlistType, err = m3u8.DecodeFrom(r, true)

	playlistURL, err := url.Parse(playlistURI)
	if err != nil {
		return
	}

	switch playlistType {
	case m3u8.MASTER:
		master := playlist.(*m3u8.MasterPlaylist)

		// A set of one or more EXT-X-MEDIA tags with the same GROUP-ID value and
		// the same TYPE value defines a Group of Renditions.
		renditionGroups := make(map[renditionGroupKey][]*m3u8.Alternative)
		for _, variant := range master.Variants {
			var variantURL *url.URL
			variantURL, err = playlistURL.Parse(variant.URI)
			if err != nil {
				return
			}
			variant.URI = variantURL.String()

			for _, alt := range variant.Alternatives {
				var altURL *url.URL
				altURL, err = playlistURL.Parse(alt.URI)
				if err != nil {
					return
				}
				alt.URI = altURL.String()

				g := renditionGroupKey{alt.Type, alt.GroupId}
				renditionGroups[g] = append(renditionGroups[g], alt)
			}
		}

		for _, v := range master.Variants {
			renditions := []renditionGroupKey{
				{"VIDEO", v.Video},
				{"AUDIO", v.Audio},
				{"SUBTITLES", v.Subtitles},
			}

			v.Alternatives = nil
			for _, g := range renditions {
				if a, ok := renditionGroups[g]; ok {
					v.Alternatives = append(v.Alternatives, a...)
				}
			}
		}

	case m3u8.MEDIA:
		media := playlist.(*m3u8.MediaPlaylist)

		var key *m3u8.Key
		media.Segments = media.Segments[:media.Count()]
		for i, seg := range media.Segments {
			seg.SeqId = media.SeqNo + uint64(i)

			var segURL *url.URL
			segURL, err = playlistURL.Parse(seg.URI)
			if err != nil {
				return
			}
			seg.URI = segURL.String()

			if seg.Key != nil {
				if strings.ToUpper(seg.Key.Method) == "NONE" {
					key = nil
				} else {
					key = seg.Key
					var keyURL *url.URL
					keyURL, err = playlistURL.Parse(key.URI)
					if err != nil {
						return
					}
					key.URI = keyURL.String()
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
