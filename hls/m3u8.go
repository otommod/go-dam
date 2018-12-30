package hls

import (
	"bytes"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafov/m3u8"
)

type CommonPlaylistTags struct {
	IndependentSegments bool
	StartOffset         time.Duration
	StartPrecise        bool
}

type MasterPlaylist struct {
	CommonPlaylistTags
	*m3u8.MasterPlaylist
}

type MediaPlaylist struct {
	TargetDuration time.Duration
	CommonPlaylistTags
	*m3u8.MediaPlaylist
}

type renditionGroupKey struct {
	Type, GroupID string
}

func splitKV(line string) []string {
	var inQuotes bool
	return strings.FieldsFunc(line, func(c rune) bool {
		switch {
		case c == ',' && !inQuotes:
			return true
		case c == '"':
			inQuotes = !inQuotes
		}
		return false
	})
}

func parseM3U8(r io.Reader, playlistURI string) (playlist m3u8.Playlist, playlistType m3u8.ListType, err error) {
	var playlistURL *url.URL
	if playlistURL, err = url.Parse(playlistURI); err != nil {
		return
	}

	var buf bytes.Buffer
	if _, err = io.Copy(&buf, r); err != nil {
		return
	}

	playlist, playlistType, err = m3u8.Decode(buf, true)
	if err != nil {
		return
	}

	var commonTags CommonPlaylistTags
	line, bufErr := buf.ReadString('\n')
	for ; bufErr == nil; line, bufErr = buf.ReadString('\n') {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "#EXT-X-INDEPENDENT-SEGMENTS"):
			commonTags.IndependentSegments = true

		case strings.HasPrefix(line, "#EXT-X-START:"):
			for _, kv := range splitKV(line[13:]) {
				switch {
				case strings.HasPrefix(kv, "TIME-OFFSET="):
					timeOffset, _ := strconv.ParseFloat(kv[12:], 64)
					commonTags.StartOffset = time.Duration(timeOffset * float64(time.Second))
				case strings.HasPrefix(kv, "PRECISE="):
					commonTags.StartPrecise = kv[8:] == "YES"
				}
			}
		}
	}

	switch playlistType {
	case m3u8.MASTER:
		master := playlist.(*m3u8.MasterPlaylist)

		// § 4.3.4.1.1
		// A set of one or more EXT-X-MEDIA tags with the same GROUP-ID value
		// and the same TYPE value defines a Group of Renditions.
		renditionGroups := make(map[renditionGroupKey][]*m3u8.Alternative)
		for _, variant := range master.Variants {
			var variantURL *url.URL
			if variantURL, err = playlistURL.Parse(variant.URI); err != nil {
				return
			}
			variant.URI = variantURL.String()

			for _, alt := range variant.Alternatives {
				var altURL *url.URL
				if altURL, err = playlistURL.Parse(alt.URI); err != nil {
					return
				}
				alt.URI = altURL.String()

				g := renditionGroupKey{alt.Type, alt.GroupId}
				renditionGroups[g] = append(renditionGroups[g], alt)
			}
		}

		for _, v := range master.Variants {
			groupKeys := []renditionGroupKey{
				{"VIDEO", v.Video},
				{"AUDIO", v.Audio},
				{"SUBTITLES", v.Subtitles},
			}

			v.Alternatives = nil
			for _, g := range groupKeys {
				if r, ok := renditionGroups[g]; ok {
					v.Alternatives = append(v.Alternatives, r...)
				}
			}
		}

		playlist = &MasterPlaylist{
			MasterPlaylist:     master,
			CommonPlaylistTags: commonTags,
		}

	case m3u8.MEDIA:
		media := playlist.(*m3u8.MediaPlaylist)

		var key *m3u8.Key
		media.Segments = media.Segments[:media.Count()]
		for i, seg := range media.Segments {
			seg.SeqId = media.SeqNo + uint64(i)

			var segURL *url.URL
			if segURL, err = playlistURL.Parse(seg.URI); err != nil {
				return
			}
			seg.URI = segURL.String()

			if seg.Map != nil {
				var mapURL *url.URL
				if mapURL, err = playlistURL.Parse(seg.Map.URI); err != nil {
					return
				}
				seg.Map.URI = mapURL.String()
			}

			if seg.Key != nil {
				if strings.ToUpper(seg.Key.Method) == "NONE" {
					key = nil
				} else {
					key = seg.Key
					var keyURL *url.URL
					if keyURL, err = playlistURL.Parse(key.URI); err != nil {
						return
					}
					key.URI = keyURL.String()
				}
			}
			seg.Key = key
		}

		playlist = &MediaPlaylist{
			TargetDuration:     time.Duration(media.TargetDuration * 1e9),
			MediaPlaylist:      media,
			CommonPlaylistTags: commonTags,
		}
	}

	return
}
