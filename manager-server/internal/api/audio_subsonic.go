package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"manager-server/internal/models"
	"manager-server/internal/repository"
)

// Subsonic API endpoint implementations

func (h *AudioHandlers) subsonicGetIndexes(c *gin.Context) {
	_, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	firstChar := "#"
	if name := strings.TrimSpace(project.Project.Name); name != "" {
		runes := []rune(strings.ToUpper(name))
		if len(runes) > 0 {
			firstRune := runes[0]
			if (firstRune >= 'A' && firstRune <= 'Z') || (firstRune >= '0' && firstRune <= '9') {
				firstChar = string(firstRune)
			}
		}
	}

	artistID := fmt.Sprintf("proj-%s", project.Project.ID)
	index := subsonicIndex{
		Name: firstChar,
		Artist: []subsonicArtist{
			{
				ID:         artistID,
				Name:       project.Project.Name,
				AlbumCount: 1,
			},
		},
	}

	resp := newSubsonicResponse()
	resp.Indexes = &subsonicIndexes{
		LastModified: time.Now().Unix() * 1000,
		Index:        []subsonicIndex{index},
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetArtists(c *gin.Context) {
	// Similar to getIndexes but with ID3 structure
	h.subsonicGetIndexes(c)
}

func (h *AudioHandlers) subsonicGetArtist(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	artistID := c.Query("id")
	if !strings.HasPrefix(artistID, "proj-") && !strings.HasPrefix(artistID, "host-") {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Artist not found"))
		return
	}

	var albums []subsonicAlbum
	var artistName string

	if strings.HasPrefix(artistID, "proj-") {
		projectID := strings.TrimPrefix(artistID, "proj-")
		if projectID != project.Project.ID {
			c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Artist not found"))
			return
		}
		artistName = project.Project.Name
		albums = append(albums, projectToSubsonicAlbum(project))
	} else if strings.HasPrefix(artistID, "host-") {
		hostName, _ := url.QueryUnescape(strings.TrimPrefix(artistID, "host-"))
		hostName = strings.TrimSpace(hostName)
		if hostName == "" {
			c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Artist not found"))
			return
		}
		filter := repository.AudioEpisodeFilter{
			ProjectID: project.Project.ID,
		}
		episodes, _, err := h.service.ListEpisodes(c.Request.Context(), actor, filter, 500, 0)
		if err != nil {
			c.XML(http.StatusOK, newSubsonicErrorResponse(0, "Failed to get episodes"))
			return
		}
		for _, ep := range episodes {
			if strings.EqualFold(ep.PrimaryHost, hostName) {
				albums = append(albums, projectToSubsonicAlbum(project))
				artistName = hostName
				break
			}
		}
	}

	if artistName == "" {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Artist not found"))
		return
	}

	resp := newSubsonicResponse()
	resp.Artist = &subsonicArtistDetail{
		ID:         artistID,
		Name:       artistName,
		AlbumCount: len(albums),
		Album:      albums,
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetAlbum(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	albumID := c.Query("id")
	if !strings.HasPrefix(albumID, "proj-") && !strings.HasPrefix(albumID, "playlist-") {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Album not found"))
		return
	}

	var songs []subsonicSong
	var albumName string
	var artistName string
	var created string

	if strings.HasPrefix(albumID, "proj-") {
		projectID := strings.TrimPrefix(albumID, "proj-")
		if projectID != project.Project.ID {
			c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Album not found"))
			return
		}

		albumName = project.Project.Name
		created = project.Project.CreateTime.Format(time.RFC3339)

		filter := repository.AudioEpisodeFilter{
			ProjectID: project.Project.ID,
			OrderBy:   "publish_at DESC",
		}
		episodes, _, err := h.service.ListEpisodes(c.Request.Context(), actor, filter, 500, 0)
		if err == nil {
			for _, ep := range episodes {
				songs = append(songs, episodeToSubsonicSong(ep))
				if artistName == "" && ep.PrimaryHost != "" {
					artistName = ep.PrimaryHost
				}
			}
		}
	}

	if albumName == "" {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Album not found"))
		return
	}

	resp := newSubsonicResponse()
	resp.Album = &subsonicAlbumDetail{
		ID:        albumID,
		Name:      albumName,
		Artist:    artistName,
		SongCount: len(songs),
		Created:   created,
		Song:      songs,
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetAlbumList(c *gin.Context) {
	h.subsonicGetAlbumList2(c)
}

func (h *AudioHandlers) subsonicGetAlbumList2(c *gin.Context) {
	_, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	resp := newSubsonicResponse()
	resp.AlbumList2 = &subsonicAlbumList2{
		Album: []subsonicAlbum{projectToSubsonicAlbum(project)},
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetSong(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	songID := c.Query("id")
	if !strings.HasPrefix(songID, "ep-") {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Song not found"))
		return
	}

	episodeIDStr := strings.TrimPrefix(songID, "ep-")
	episodeID, err := strconv.ParseUint(episodeIDStr, 10, 64)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Invalid song ID"))
		return
	}

	episode, err := h.service.GetEpisode(c.Request.Context(), actor, episodeID)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Song not found"))
		return
	}
	if episode.ProjectID != project.Project.ID {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Song not found"))
		return
	}

	resp := newSubsonicResponse()
	resp.Song = &subsonicSong{}
	*resp.Song = episodeToSubsonicSong(episode)
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicSearch2(c *gin.Context) {
	h.subsonicSearch3(c)
}

func (h *AudioHandlers) subsonicSearch3(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	query := strings.TrimSpace(c.Query("query"))
	queryLower := strings.ToLower(query)
	artistCount, _ := strconv.Atoi(c.DefaultQuery("artistCount", "20"))
	albumCount, _ := strconv.Atoi(c.DefaultQuery("albumCount", "20"))
	songCount, _ := strconv.Atoi(c.DefaultQuery("songCount", "20"))

	var artists []subsonicArtist
	var albums []subsonicAlbum
	var songs []subsonicSong

	if query == "" || strings.Contains(strings.ToLower(project.Project.Name), queryLower) {
		albums = append(albums, projectToSubsonicAlbum(project))
		artists = append(artists, subsonicArtist{
			ID:         fmt.Sprintf("proj-%s", project.Project.ID),
			Name:       project.Project.Name,
			AlbumCount: 1,
		})
	}

	filter := repository.AudioEpisodeFilter{
		ProjectID: project.Project.ID,
		Query:     query,
	}
	episodes, _, err := h.service.ListEpisodes(c.Request.Context(), actor, filter, songCount, 0)
	if err == nil {
		for _, ep := range episodes {
			songs = append(songs, episodeToSubsonicSong(ep))
		}
	}

	if len(artists) > artistCount {
		artists = artists[:artistCount]
	}
	if len(albums) > albumCount {
		albums = albums[:albumCount]
	}

	resp := newSubsonicResponse()
	resp.SearchResult3 = &subsonicSearchResult3{
		Artist: artists,
		Album:  albums,
		Song:   songs,
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicDownload(c *gin.Context) {
	h.serveSubsonicStream(c, true)
}

func (h *AudioHandlers) subsonicGetCoverArt(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		c.String(http.StatusUnauthorized, "Authentication required")
		return
	}

	coverID := c.Query("id")
	// size := c.Query("size") // TODO: Support resizing

	// Handle different cover types
	if strings.HasPrefix(coverID, "ep-") {
		episodeIDStr := strings.TrimPrefix(coverID, "ep-")
		episodeID, err := strconv.ParseUint(episodeIDStr, 10, 64)
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid cover ID")
			return
		}

		episode, err := h.service.GetEpisode(c.Request.Context(), actor, episodeID)
		if err != nil || episode.ProjectID != project.Project.ID || episode.CoverURI == "" {
			c.String(http.StatusNotFound, "Cover not found")
			return
		}

		// Redirect to cover URI or serve file
		c.Redirect(http.StatusFound, episode.CoverURI)
		return
	}

	c.String(http.StatusNotFound, "Cover not found")
}

// Placeholder implementations for remaining endpoints

func (h *AudioHandlers) subsonicGetPlaylists(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	filter := repository.AudioPlaylistFilter{
		ProjectID: project.Project.ID,
	}
	pls, _, err := h.service.ListPlaylists(c.Request.Context(), actor, filter, 100, 0)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(0, "Failed to get playlists"))
		return
	}

	var playlists []subsonicPlaylist
	for _, pl := range pls {
		playlists = append(playlists, subsonicPlaylist{
			ID:        fmt.Sprintf("playlist-%d", pl.ID),
			Name:      pl.Name,
			SongCount: 0,
			Duration:  0,
			Created:   pl.CreateTime.Format(time.RFC3339),
			Changed:   pl.UpdateTime.Format(time.RFC3339),
			CoverArt:  pl.CoverURI,
		})
	}

	resp := newSubsonicResponse()
	resp.Playlists = &subsonicPlaylists{
		Playlist: playlists,
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetPlaylist(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	playlistID := c.Query("id")
	if !strings.HasPrefix(playlistID, "playlist-") {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Playlist not found"))
		return
	}

	plIDStr := strings.TrimPrefix(playlistID, "playlist-")
	plID, err := strconv.ParseUint(plIDStr, 10, 64)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Invalid playlist ID"))
		return
	}

	filter := repository.AudioPlaylistFilter{ProjectID: project.Project.ID}
	playlists, _, err := h.service.ListPlaylists(c.Request.Context(), actor, filter, 100, 0)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(0, "Failed to get playlists"))
		return
	}
	var playlist *models.AudioPlaylist
	for _, pl := range playlists {
		if pl.ID == plID {
			playlist = pl
			break
		}
	}
	if playlist == nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Playlist not found"))
		return
	}

	resp := newSubsonicResponse()
	resp.Playlist = &subsonicPlaylistDetail{
		ID:   playlistID,
		Name: playlist.Name,
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicCreatePlaylist(c *gin.Context) {
	c.XML(http.StatusOK, newSubsonicErrorResponse(30, "Not implemented"))
}

func (h *AudioHandlers) subsonicUpdatePlaylist(c *gin.Context) {
	c.XML(http.StatusOK, newSubsonicErrorResponse(30, "Not implemented"))
}

func (h *AudioHandlers) subsonicDeletePlaylist(c *gin.Context) {
	c.XML(http.StatusOK, newSubsonicErrorResponse(30, "Not implemented"))
}

func (h *AudioHandlers) subsonicStar(c *gin.Context) {
	c.XML(http.StatusOK, newSubsonicErrorResponse(30, "Not implemented"))
}

func (h *AudioHandlers) subsonicUnstar(c *gin.Context) {
	c.XML(http.StatusOK, newSubsonicErrorResponse(30, "Not implemented"))
}

func (h *AudioHandlers) subsonicGetStarred(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.Starred = &subsonicStarred{}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetStarred2(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.Starred2 = &subsonicStarred2{}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicScrobble(c *gin.Context) {
	resp := newSubsonicResponse()
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetNowPlaying(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.NowPlaying = &subsonicNowPlaying{}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicSetRating(c *gin.Context) {
	resp := newSubsonicResponse()
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetGenres(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.Genres = &subsonicGenres{
		Genre: []subsonicGenre{
			{SongCount: 0, AlbumCount: 0, Value: "Podcast"},
		},
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetSongsByGenre(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.SongsByGenre = &subsonicSongs{}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetRandomSongs(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if size > 500 {
		size = 500
	}

	filter := repository.AudioEpisodeFilter{
		ProjectID: project.Project.ID,
		OrderBy:   "RANDOM()",
	}
	episodes, _, err := h.service.ListEpisodes(c.Request.Context(), actor, filter, size, 0)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(0, "Failed to get songs"))
		return
	}

	var songs []subsonicSong
	for _, ep := range episodes {
		songs = append(songs, episodeToSubsonicSong(ep))
	}

	resp := newSubsonicResponse()
	resp.RandomSongs = &subsonicSongs{
		Song: songs,
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetSimilarSongs(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.SimilarSongs = &subsonicSimilarSongs{}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetSimilarSongs2(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.SimilarSongs2 = &subsonicSimilarSongs2{}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetTopSongs(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.TopSongs = &subsonicTopSongs{}
	c.XML(http.StatusOK, resp)
}
