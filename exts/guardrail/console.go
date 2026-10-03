package guardrail

import (
	"fmt"
	agentsession "github.com/chainreactors/cyber/agent/session"

	"github.com/chainreactors/cyber/core/extension"

	"github.com/chainreactors/cyber/core/operation"
	"github.com/chainreactors/cyber/pkg/console/api"
	"github.com/spf13/cobra"
)

type ConsoleExtension struct{}

func NewConsole() *ConsoleExtension { return &ConsoleExtension{} }
func (*ConsoleExtension) Load(scope *extension.Scope) error {
	runtime, err := extension.Use[*Runtime](scope)
	if err != nil {
		return err
	}
	sessions, err := extension.Use[*agentsession.Runtime](scope)
	if err != nil {
		return err
	}
	return extension.Add(scope, consoleBindings(runtime, sessions))
}
func consoleBindings(runtime *Runtime, sessions *agentsession.Runtime) *api.Bindings {
	return &api.Bindings{Commands: func(view api.View) []*cobra.Command {
		root := &cobra.Command{Use: "/guardrail", Short: "Inspect and resolve waiting tool approvals"}
		var session string
		root.PersistentFlags().StringVar(&session, "session", "", "Session scope (defaults to the attached session)")
		sessionID := func() string {
			if session != "" {
				return session
			}
			if view.SessionID != nil {
				return view.SessionID()
			}
			return ""
		}
		root.AddCommand(&cobra.Command{Use: "pending", Short: "List pending tool reviews", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			for _, review := range sessionReviews(runtime, sessions, sessionID()) {
				fmt.Fprintf(view.Out, "%s  %s  %s\nSession: %s  Directory: %s\n%s\nArguments: %s\n", review.Operation.OperationId, review.Call.Name, review.ExpiresAt.AsTime().Format("15:04:05"), review.SessionId, review.Call.WorkingDirectory, review.Decision.Reason, review.Call.GetArguments().GetData())
			}
			return nil
		}})
		for _, verb := range []string{"approve", "reject"} {
			root.AddCommand(&cobra.Command{Use: verb + " <operation-id>", Short: verb + " a pending tool invocation", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
				owner := reviewSession(runtime, sessions, sessionID(), args[0])
				if owner == "" {
					return fmt.Errorf("pending review not found in this session")
				}
				ctx := operation.ContextWithInvocation(cmd.Context(), operation.Invocation{SessionID: owner, Emitter: "cli"})
				if err := runtime.Resolve(ctx, args[0], verb == "approve"); err != nil {
					return err
				}
				fmt.Fprintln(view.Out, "Guardrail review resolved: "+verb)
				return nil
			}})
		}
		return []*cobra.Command{root}
	}}
}
