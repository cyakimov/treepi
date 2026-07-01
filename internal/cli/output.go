package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cyakimov/treepi/internal/core"
	"github.com/cyakimov/treepi/internal/exit"
)

// envelope is the stable --json shape emitted by every command.
type envelope struct {
	TreepiVersion string   `json:"treepi_version"`
	Op            string   `json:"op"`
	OK            bool     `json:"ok"`
	Data          any      `json:"data,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	Error         *errEnv  `json:"error,omitempty"`
}

type errEnv struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func emitJSON(w io.Writer, op string, data any) error {
	return emitJSONWarn(w, op, data, nil)
}

// emitJSONWarn emits the success envelope including any operation warnings.
func emitJSONWarn(w io.Writer, op string, data any, warnings []string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope{TreepiVersion: version, Op: op, OK: true, Data: data, Warnings: warnings})
}

// emitJSONErr emits one envelope for a best-effort command: on success it is the
// ok:true envelope (data + warnings); on failure it reports ok:false while still
// carrying data - what did succeed - alongside the error.
func emitJSONErr(w io.Writer, op string, data any, warnings []string, err error) error {
	if err == nil {
		return emitJSONWarn(w, op, data, warnings)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope{TreepiVersion: version, Op: op, OK: false, Data: data, Warnings: warnings, Error: errToEnv(err)})
}

// errToEnv maps an error to the JSON error envelope: a typed *exit.Error
// contributes its stable Reason as the code, otherwise the code is "internal".
func errToEnv(err error) *errEnv {
	code, msg := "internal", err.Error()
	var te *exit.Error
	if errors.As(err, &te) {
		if te.Reason != "" {
			code = te.Reason
		}
		msg = te.Error()
	}
	return &errEnv{Code: code, Message: msg}
}

// fail renders err (a JSON error envelope to stdout in --json mode, otherwise a
// line to stderr) and returns it so main can map the exit code.
func fail(cmd *cobra.Command, op string, err error) error {
	if jsonMode(cmd) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		_ = enc.Encode(envelope{TreepiVersion: version, Op: op, OK: false, Error: errToEnv(err)})
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "treepi: "+err.Error())
	}
	return err
}

// renderTasks prints the static worktree table.
func renderTasks(w io.Writer, tasks []core.TaskInfo) {
	if len(tasks) == 0 {
		fmt.Fprintln(w, "no worktrees yet - `treepi new <type> <task>` to create one")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "TASK\tTYPE\tBRANCH\tSTATUS\tA/B\tPATH")
	for _, t := range tasks {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d/%d\t%s\n",
			t.Task, t.Type, t.Branch, t.Status, t.Ahead, t.Behind, t.Path)
	}
	_ = tw.Flush()
}
