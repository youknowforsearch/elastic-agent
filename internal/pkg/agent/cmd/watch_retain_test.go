// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/ttl"
	"github.com/elastic/elastic-agent/pkg/core/logger/loggertest"
)

func TestFailedHomeToRetain(t *testing.T) {
	failedHome := filepath.Join("data", "elastic-agent-9.3.0-newver")
	prevHome := filepath.Join("data", "elastic-agent-9.2.0-prvver")

	assert.Equal(t, failedHome, failedHomeToRetain(&upgrade.UpdateMarker{
		Hash: "newver", VersionedHome: failedHome, PrevVersionedHome: prevHome,
	}))
	assert.Equal(t, "", failedHomeToRetain(&upgrade.UpdateMarker{
		Hash: "newver", VersionedHome: prevHome, PrevVersionedHome: prevHome,
	}), "the home we roll back to must never be retained")
	assert.Equal(t, "", failedHomeToRetain(&upgrade.UpdateMarker{PrevVersionedHome: prevHome}), "no failed home in the marker")
}

func TestRetainFailedUpgradeLogs(t *testing.T) {
	log, _ := loggertest.New(t.Name())
	failedHome := filepath.Join("data", "elastic-agent-9.3.0-newver")
	marker := &upgrade.UpdateMarker{Version: "9.3.0", Hash: "newver", VersionedHome: failedHome}

	t.Run("writes the logs only marker", func(t *testing.T) {
		topDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(topDir, failedHome), 0o750))

		before := time.Now()
		retainFailedUpgradeLogs(log, topDir, marker, failedHome, time.Hour)

		got, malformed, err := ttl.NewTTLMarkerRegistry(log, topDir).GetAll()
		require.NoError(t, err)
		assert.Empty(t, malformed)
		require.Contains(t, got, failedHome)
		assert.Equal(t, "9.3.0", got[failedHome].Version)
		assert.Equal(t, "newver", got[failedHome].Hash)
		assert.True(t, got[failedHome].LogsOnly)
		assert.WithinDuration(t, before.Add(time.Hour), got[failedHome].ValidUntil, 5*time.Second)
	})

	t.Run("no home to retain: nothing is written", func(t *testing.T) {
		topDir := t.TempDir()
		retainFailedUpgradeLogs(log, topDir, marker, "", time.Hour)

		got, _, err := ttl.NewTTLMarkerRegistry(log, topDir).GetAll()
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("a write failure does not panic nor propagate", func(t *testing.T) {
		// the failed home does not exist so the marker cannot be written
		assert.NotPanics(t, func() { retainFailedUpgradeLogs(log, t.TempDir(), marker, failedHome, time.Hour) })
	})
}
