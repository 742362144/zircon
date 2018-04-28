package rpc

import (
	"zircon/apis"
	"zircon/rpc/twirp"
	"net/http"
	"context"
	"fmt"
	"net"
	"errors"
)

// Connects to an RPC handler for a Chunkserver on a certain address.
func UncachedSubscribeChunkserver(address apis.ServerAddress, client *http.Client) (apis.Chunkserver, error) {
	saddr := "http://" + string(address)
	tserve := twirp.NewChunkserverProtobufClient(saddr, client)

	return &proxyTwirpAsChunkserver{server: tserve}, nil
}

// Starts serving an RPC handler for a Chunkserver on a certain address. Runs forever.
func PublishChunkserver(server apis.Chunkserver, address string) (func(kill bool) error, apis.ServerAddress, error) {
	tserve := twirp.NewChunkserverServer(&proxyChunkserverAsTwirp{server: server}, nil)

	if address == "" {
		address = ":http"
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, "", err
	}

	httpServer := &http.Server{Handler: tserve}
	termErr := make(chan error)
	go func() {
		defer func() {
			err := recover()
			termErr <- fmt.Errorf("panic: %v", err)
		}()

		err := httpServer.Serve(listener)

		if err == http.ErrServerClosed {
			err = nil
		}
		termErr <- err
	}()

	teardown := func(kill bool) error {
		var err1 error
		if kill {
			err1 = httpServer.Shutdown(context.Background())
			if err1 == nil {
				err1 = listener.Close()
			}
		}
		err2 := <-termErr
		if err1 == nil {
			return err2
		} else if err2 == nil {
			return err1
		} else {
			return fmt.Errorf("multiple errors: { %v } and { %v }", err1, err2)
		}
	}

	return teardown, apis.ServerAddress(listener.Addr().String()), nil
}

type proxyChunkserverAsTwirp struct {
	server apis.Chunkserver
}

func (p *proxyChunkserverAsTwirp) StartWriteReplicated(context context.Context, input *twirp.Chunkserver_StartWriteReplicated) (*twirp.Nothing, error) {
	addresses := make([]apis.ServerAddress, len(input.Address))
	for i, v := range input.Address {
		addresses[i] = apis.ServerAddress(v)
	}

	err := p.server.StartWriteReplicated(apis.ChunkNum(input.Chunk), apis.Offset(input.Offset), input.Data, addresses)
	return &twirp.Nothing{}, err
}

func (p *proxyChunkserverAsTwirp) Replicate(context context.Context, input *twirp.Chunkserver_Replicate) (*twirp.Nothing, error) {
	err := p.server.Replicate(apis.ChunkNum(input.Chunk), apis.ServerAddress(input.ServerAddress), apis.Version(input.Version))
	return &twirp.Nothing{}, err
}

func (p *proxyChunkserverAsTwirp) Read(context context.Context, input *twirp.Chunkserver_Read) (*twirp.Chunkserver_Read_Result, error) {
	data, version, err := p.server.Read(apis.ChunkNum(input.Chunk), apis.Offset(input.Offset), apis.Length(input.Length), apis.Version(input.Version))
	message := ""
	if err != nil {
		message = err.Error()
		if message == "" {
			panic("expected nonempty error code")
		}
	}
	return &twirp.Chunkserver_Read_Result{
		Data: data,
		Version: uint64(version),
		Error: message,
	}, nil
}

func (p *proxyChunkserverAsTwirp) StartWrite(context context.Context, input *twirp.Chunkserver_StartWrite) (*twirp.Nothing, error) {
	err := p.server.StartWrite(apis.ChunkNum(input.Chunk), apis.Offset(input.Offset), input.Data)
	return &twirp.Nothing{}, err
}

func (p *proxyChunkserverAsTwirp) CommitWrite(context context.Context, input *twirp.Chunkserver_CommitWrite) (*twirp.Nothing, error) {
	err := p.server.CommitWrite(apis.ChunkNum(input.Chunk), apis.CommitHash(input.Hash), apis.Version(input.OldVersion), apis.Version(input.NewVersion))
	return &twirp.Nothing{}, err
}

func (p *proxyChunkserverAsTwirp) UpdateLatestVersion(context context.Context, input *twirp.Chunkserver_UpdateLatestVersion) (*twirp.Nothing, error) {
	err := p.server.UpdateLatestVersion(apis.ChunkNum(input.Chunk), apis.Version(input.OldVersion), apis.Version(input.NewVersion))
	return &twirp.Nothing{}, err
}

func (p *proxyChunkserverAsTwirp) Add(context context.Context, input *twirp.Chunkserver_Add) (*twirp.Nothing, error) {
	err := p.server.Add(apis.ChunkNum(input.Chunk), input.InitialData, apis.Version(input.Version))
	return &twirp.Nothing{}, err
}

func (p *proxyChunkserverAsTwirp) Delete(context context.Context, input *twirp.Chunkserver_Delete) (*twirp.Nothing, error) {
	err := p.server.Delete(apis.ChunkNum(input.Chunk), apis.Version(input.Version))
	return &twirp.Nothing{}, err
}

func (p *proxyChunkserverAsTwirp) ListAllChunks(context.Context,
		*twirp.Nothing) (*twirp.Chunkserver_ListAllChunks_Result, error) {
	chunks, err := p.server.ListAllChunks()

	chunkVersions := make([]*twirp.ChunkVersion, len(chunks))
	for i, chunk := range chunks {
		chunkVersions[i] = &twirp.ChunkVersion{
			Chunk: uint64(chunk.Chunk),
			Version: uint64(chunk.Version),
		}
	}

	return &twirp.Chunkserver_ListAllChunks_Result{
		Chunks: chunkVersions,
	}, err
}

type proxyTwirpAsChunkserver struct {
	server twirp.Chunkserver
}

func (p *proxyTwirpAsChunkserver) StartWriteReplicated(chunk apis.ChunkNum, offset apis.Offset, data []byte,
		replicas []apis.ServerAddress) (error) {
	addresses := make([]string, len(replicas))
	for i, v := range replicas {
		addresses[i] = string(v)
	}
	_, err := p.server.StartWriteReplicated(context.Background(), &twirp.Chunkserver_StartWriteReplicated{
		Chunk: uint64(chunk),
		Offset: uint32(offset),
		Data: data,
		Address: addresses,
	})
	return err
}

func (p *proxyTwirpAsChunkserver) Replicate(chunk apis.ChunkNum, serverAddress apis.ServerAddress,
		version apis.Version) (error) {
	_, err := p.server.Replicate(context.Background(), &twirp.Chunkserver_Replicate{
		Chunk: uint64(chunk),
		ServerAddress: string(serverAddress),
		Version: uint64(version),
	})
	return err
}

func (p *proxyTwirpAsChunkserver) Read(chunk apis.ChunkNum, offset apis.Offset, length apis.Length, minimum apis.Version) ([]byte, apis.Version, error) {
	result, err := p.server.Read(context.Background(), &twirp.Chunkserver_Read{
		Chunk: uint64(chunk),
		Offset: uint32(offset),
		Length: uint32(length),
		Version: uint64(minimum),
	})
	if err != nil {
		return nil, 0, err
	}
	if result.Error != "" {
		return nil, apis.Version(result.Version), errors.New(result.Error)
	}
	return result.Data, apis.Version(result.Version), nil
}

func (p *proxyTwirpAsChunkserver) StartWrite(chunk apis.ChunkNum, offset apis.Offset, data []byte) (error) {
	_, err := p.server.StartWrite(context.Background(), &twirp.Chunkserver_StartWrite{
		Chunk: uint64(chunk),
		Offset: uint32(offset),
		Data: data,
	})
	return err
}

func (p *proxyTwirpAsChunkserver) CommitWrite(chunk apis.ChunkNum, hash apis.CommitHash, oldVersion apis.Version,
		newVersion apis.Version) (error) {
	_, err := p.server.CommitWrite(context.Background(), &twirp.Chunkserver_CommitWrite{
		Chunk: uint64(chunk),
		Hash: string(hash),
		OldVersion: uint64(oldVersion),
		NewVersion: uint64(newVersion),
	})
	return err
}

func (p *proxyTwirpAsChunkserver) UpdateLatestVersion(chunk apis.ChunkNum, oldVersion apis.Version,
		newVersion apis.Version) error {
	_, err := p.server.UpdateLatestVersion(context.Background(), &twirp.Chunkserver_UpdateLatestVersion{
		Chunk: uint64(chunk),
		OldVersion: uint64(oldVersion),
		NewVersion: uint64(newVersion),
	})
	return err
}

func (p *proxyTwirpAsChunkserver) Add(chunk apis.ChunkNum, initialData []byte, initialVersion apis.Version) (error) {
	_, err := p.server.Add(context.Background(), &twirp.Chunkserver_Add{
		Chunk: uint64(chunk),
		InitialData: initialData,
		Version: uint64(initialVersion),
	})
	return err
}

func (p *proxyTwirpAsChunkserver) Delete(chunk apis.ChunkNum, version apis.Version) (error) {
	_, err := p.server.Delete(context.Background(), &twirp.Chunkserver_Delete{
		Chunk: uint64(chunk),
		Version: uint64(version),
	})
	return err
}

func (p *proxyTwirpAsChunkserver) ListAllChunks() ([]struct{ Chunk apis.ChunkNum; Version apis.Version }, error) {
	result, err := p.server.ListAllChunks(context.Background(), &twirp.Nothing{})
	decoded := make([]struct{ Chunk apis.ChunkNum; Version apis.Version }, len(result.Chunks))
	for i, v := range result.Chunks {
		decoded[i] = struct {
			Chunk   apis.ChunkNum
			Version apis.Version
		}{Chunk: apis.ChunkNum(v.Chunk), Version: apis.Version(v.Version)}
	}
	return decoded, err
}