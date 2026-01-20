package models

import (
	dbmodels "manager-server/internal/models"
)

// Project aliases the persistence layer struct to expose audio project data.
type Project = dbmodels.AudioProject

// Episode aliases the persistence layer struct to expose episode metadata.
type Episode = dbmodels.AudioEpisode

// Playlist aliases the persistence layer struct to expose playlist metadata.
type Playlist = dbmodels.AudioPlaylist

// PlaylistEpisode aliases the persistence layer struct representing playlist entries.
type PlaylistEpisode = dbmodels.AudioPlaylistEpisode

// Job aliases the persistence layer struct to expose asynchronous job state.
type Job = dbmodels.AudioJob
