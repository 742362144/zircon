package chunkserver

import (
	"github.com/stretchr/testify/require"
	"testing"
	"zircon/apis"
	"zircon/chunkserver/control"
	"zircon/chunkserver/storage"
	"zircon/rpc"
)

// returns number of bytes of storage used, at a rough approximation
type StorageStats func() int

func NewTestChunkserver(t *testing.T, cache rpc.ConnectionCache) (apis.Chunkserver, StorageStats, control.Teardown) {
	mem, err := storage.ConfigureMemoryStorage()
	require.NoError(t, err)
	single, teardown, err := control.ExposeChunkserver(mem)
	require.NoError(t, err)
	server, err := WithChatter(single, cache)
	require.NoError(t, err)

	stats := mem.(*storage.MemoryStorage).StatsForTesting

	return server, stats, func() {
		teardown()
		mem.Close()
	}
}
