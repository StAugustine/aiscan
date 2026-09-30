package node

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/agent/session"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
	types "github.com/chainreactors/cyber/core/types"
	cfg "github.com/chainreactors/cyber/pkg/config"
	"github.com/chainreactors/cyber/pkg/harness"
	"github.com/chainreactors/cyber/pkg/profile"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

type recoveryProvider struct{ onChat func() }

func (*recoveryProvider) Name() string { return "openai" }
func (p *recoveryProvider) ChatCompletion(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	if p.onChat != nil {
		p.onChat()
	}
	return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", "ok")}}}, nil
}

type recoveryProfile struct {
	*reloadTestProfile
	onChat func()
}

func (p *recoveryProfile) ReloadProvider(_ context.Context, config provider.ProviderConfig) error {
	state, err := p.Providers()
	if err == nil {
		state.Set(&recoveryProvider{onChat: p.onChat}, config)
	}
	return err
}

// Exercise the node transport, not just the runtime setter: RC6 builds a new
// profile on this path even when an extension can apply the model in place.
func TestModelReloadKeepsProfileConnectionAndConversation(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		t.Run(fmt.Sprintf("in_place_startup=%v", inPlace), func(t *testing.T) {
			testModelReloadKeepsConversation(t, inPlace)
		})
	}
}

func testModelReloadKeepsConversation(t *testing.T, inPlace bool) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	option := &cfg.Option{Explicit: map[string]bool{}, NodeOptions: cfg.NodeOptions{NodeID: "recovery"}}
	if inPlace {
		var err error
		option, err = cfg.ResolveDistributedRuntime(&types.DistributeConfig{}, option)
		if err != nil {
			t.Fatal(err)
		}
		option.Prompt = "run the startup task after the first provider is enabled"
	}
	var mu sync.Mutex
	startup := make(chan struct{})
	var started sync.Once
	onChat := func() { started.Do(func() { close(startup) }) }
	var built []*recoveryProfile
	build := func(request profile.Request) (profile.Profile, error) {
		h, err := harness.New(harness.Config{
			Base:    harness.BaseConfig{Directory: t.TempDir(), Provider: provider.StartupConfig{Mode: provider.StartupDisabled}},
			Session: request.Session,
			Extensions: []extension.Extension{extension.Func{LoadFunc: func(scope *extension.Scope) error {
				state, err := extension.Use[*provider.State](scope)
				if err == nil && request.ProviderMode != profile.ProviderDisabled {
					state.Set(&recoveryProvider{onChat: onChat}, cfg.ProviderConfig(request.Option))
				}
				return err
			}}},
		})
		if err != nil {
			return nil, err
		}
		p := &recoveryProfile{reloadTestProfile: &reloadTestProfile{Harness: h}, onChat: onChat}
		mu.Lock()
		built = append(built, p)
		mu.Unlock()
		return p, nil
	}
	completed := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			completed <- err
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
		receive := func() (*aop.Envelope, error) {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return nil, err
			}
			envelope := new(aop.Envelope)
			return envelope, proto.Unmarshal(data, envelope)
		}
		send := func(id, reply string, message proto.Message) error {
			data, err := proto.Marshal(aop.MustWrap(id, reply, message))
			if err != nil {
				return err
			}
			return conn.WriteMessage(websocket.BinaryMessage, data)
		}
		hello, err := receive()
		if err != nil {
			completed <- err
			return
		}
		if err := send("accepted", hello.Id, &aop.ProtocolMessage{Message: &aop.ProtocolMessage_AgentAccepted{AgentAccepted: &aop.AgentAccepted{NodeId: "recovery"}}}); err != nil {
			completed <- err
			return
		}
		reload := func(id, model string) error {
			config := &types.DistributeConfig{Llm: &types.LLMConfig{ActiveProfile: "primary", Providers: []*types.LLMProviderConfig{{Id: "primary", Provider: "openai", BaseUrl: "https://fixture.invalid/v1", ApiKey: "fixture", Model: model}}}}
			if err := send(id, "", &types.ReloadProtocolMessage{Message: &types.ReloadProtocolMessage_Request{Request: &types.ReloadRequest{Config: config}}}); err != nil {
				return err
			}
			for {
				envelope, err := receive()
				if err != nil {
					return err
				}
				if envelope.ReplyTo != id {
					continue
				}
				message, err := aop.Unwrap(envelope)
				if err != nil {
					return err
				}
				result, ok := message.(*types.ReloadProtocolMessage)
				if !ok || !result.GetResult().GetOk() {
					return fmt.Errorf("reload failed: %v", message)
				}
				return nil
			}
		}
		mu.Lock()
		configured := len(built) > 1
		mu.Unlock()
		if !configured {
			// A graph change reconnects; a provider-only startup keeps this
			// connection and must still release the initial task.
			if err := reload("initial", "model-a"); err != nil {
				completed <- err
				return
			}
			if !inPlace {
				for {
					if _, err := receive(); err != nil {
						return
					}
				}
			}
		}
		mu.Lock()
		current, count := built[len(built)-1], len(built)
		mu.Unlock()
		rt, err := current.Runtime()
		if err != nil {
			completed <- err
			return
		}
		if inPlace {
			if count != 1 {
				completed <- fmt.Errorf("provider-only startup rebuilt the profile: builds=%d", count)
				return
			}
			select {
			case <-startup:
			case <-ctx.Done():
				completed <- fmt.Errorf("startup task was not released: %w", ctx.Err())
				return
			}
		}
		conversation, err := rt.OpenSession(ctx, session.SessionOptions{ID: "customer", Messages: []*aop.Message{provider.TextMessage("user", "remember the uploaded HAR")}})
		if err != nil {
			completed <- err
			return
		}
		if err := reload("switch", "model-b"); err != nil {
			completed <- err
			return
		}
		mu.Lock()
		after := len(built)
		mu.Unlock()
		if after != count || current.closed.Load() {
			completed <- fmt.Errorf("model switch rebuilt profile: builds %d -> %d, closed=%v", count, after, current.closed.Load())
			return
		}
		if conversation.Model() != "model-b" || len(conversation.MessagesSnapshot()) != 1 {
			completed <- fmt.Errorf("model or transcript lost: model=%q messages=%v", conversation.Model(), conversation.MessagesSnapshot())
			return
		}
		// Re-sending the same settings must also keep the existing connection.
		completed <- reload("duplicate", "model-b")
		<-ctx.Done()
	}))
	defer server.Close()
	option.ServerURL = server.URL
	nodeDone := make(chan error, 1)
	go func() {
		nodeDone <- RunWebSocket(ctx, build, option, telemetry.NopLogger())
	}()
	select {
	case err := <-completed:
		cancel()
		if err != nil {
			t.Error(err)
		}
	case err := <-nodeDone:
		cancel()
		t.Fatalf("node stopped: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-nodeDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("node shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("node did not stop after cancellation")
	}
}
