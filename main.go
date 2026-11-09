package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joshuarubin/go-sway"
	"github.com/jtagcat/util/std"
)

var (
	IGNORE_APP_IDS    = make(map[string]bool)
	WORKSPACE_TIMEOUT = 90 * time.Second
)

func init() {
	for _, app_id := range strings.Split(os.Getenv("IGNORE_APP_IDS"), ",") {
		IGNORE_APP_IDS[app_id] = true
	}

	if durStr, ok := os.LookupEnv("WORKSPACE_TIMEOUT"); ok {
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			WORKSPACE_TIMEOUT = dur
		}
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client, err := sway.New(ctx)
	if err != nil {
		slog.Error("opening sway client", std.SlogErr(err))
		os.Exit(1)
	}

	if err := sway.Subscribe(
		ctx,
		evHandler{
			EventHandler: sway.NoOpEventHandler(),
			client:       client,
			timers:       make(workspaceTimers),
		},
		sway.EventTypeWorkspace, sway.EventTypeWindow,
	); err != nil {
		slog.Error("subscribing to sway events", std.SlogErr(err))
		os.Exit(1)
	}
}

type evHandler struct {
	sway.EventHandler

	client sway.Client
	timers workspaceTimers
}
type workspaceTimers map[int64]*time.Timer

func (evHandler) Window(_ context.Context, e sway.WindowEvent) {
	if e.Change == sway.WindowFocus && e.Container.PID != nil {
		pid := int(*e.Container.PID)

		slog.Debug("resuming focused window", slog.Int("PID", pid))

		if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
			slog.Error("failed to SIGCONT", slog.Int("PID", pid))
		}
	}
}

func (eh evHandler) Workspace(ctx context.Context, e sway.WorkspaceEvent) {
	if e.Change != sway.WorkspaceFocus {
		return
	}

	slog.Debug("workspace focus changed", slog.Int64("current", e.Current.ID), slog.String("currentName", e.Current.Name), slog.Int64("oldID", e.Old.ID), slog.String("oldName", e.Old.Name))

	if e.Current != nil && eh.timers[e.Current.ID] != nil {
		eh.timers[e.Current.ID].Stop()
	}

	if e.Old != nil {
		if eh.timers[e.Old.ID] != nil {
			eh.timers[e.Old.ID].Reset(WORKSPACE_TIMEOUT)
		} else {
			eh.timers[e.Old.ID] = time.AfterFunc(WORKSPACE_TIMEOUT, func() { eh.suspendWorkspace(ctx, e.Old.ID) })
		}
	}
}

func (eh evHandler) suspendWorkspace(ctx context.Context, id int64) {
	tree, _ := eh.client.GetTree(ctx)
	ws := findNode(tree, id)

	if ws == nil {
		return // workspace destroyed
	}

	if ws.Focused {
		return // TOCTOU
	}

	slog.Info("suspending workspace", slog.Int64("id", id), slog.Bool("found", ws != nil), slog.String("name", ws.Name))
	suspendChildren(tree, ws)
}

func findNode(node *sway.Node, id int64) *sway.Node {
	if node.ID == id {
		return node
	}

	for _, child := range node.Nodes {
		if childResult := findNode(child, id); childResult != nil {
			return childResult
		}
	}

	return nil
}

func suspendChildren(tree *sway.Node, node *sway.Node) {
	if node.PID != nil {
		pid := int(*node.PID)

		if checkIgnored(node) {
			return
		}

		if pidActive(tree, *node.PID) {
			return
		}

		slog.Debug("suspending process", slog.Int("PID", pid), slog.String("app_id", *node.AppID))

		if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
			slog.Error("failed to SIGSTOP", slog.Int("PID", pid))
		}

		return
	}

	for _, child := range node.Nodes {
		suspendChildren(tree, child)
	}
}

func checkIgnored(node *sway.Node) bool {
	if node.AppID == nil {
		return false
	}

	ignored := IGNORE_APP_IDS[*node.AppID]
	if ignored {
		slog.Debug("ignoring suspend", slog.String("app_id", *node.AppID))
	}

	return ignored
}

func pidActive(tree *sway.Node, pid uint32) bool {
	for _, pidHolder := range getPID(tree, pid) {
		if *pidHolder.Visible {
			slog.Debug("not suspending as another window with same PID visible", slog.Uint64("pid", uint64(pid)))
			return true
		}

		if pidHolder.IdleInhibitors.Application != "none" || pidHolder.IdleInhibitors.User != "none" {
			slog.Debug("not suspending as another window with same PID hsa inhibitor", slog.Uint64("pid", uint64(pid)))
			return true
		}
	}

	return false
}

func getPID(tree *sway.Node, pid uint32) (nodes []*sway.Node) {
	if tree.PID != nil && *tree.PID == pid {
		nodes = append(nodes, tree)
		return
	}

	for _, child := range tree.Nodes {
		nodes = append(nodes, getPID(child, pid)...)
	}

	return nodes
}
