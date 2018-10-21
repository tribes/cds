package main

import (
	"bufio"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/ovh/cds/sdk"

	"github.com/ovh/cds/cli"
	"github.com/spf13/cobra"
)

var (
	templateCmd = cli.Command{
		Name:  "template",
		Short: "Manage CDS workflow template",
	}

	template = cli.NewCommand(templateCmd, nil,
		[]*cobra.Command{
			cli.NewCommand(templateExecuteCmd, templateExecuteRun, nil, withAllCommandModifiers()...),
			cli.NewCommand(templateUpdateCmd, templateUpdateRun, nil, withAllCommandModifiers()...),
		})
)

var templateExecuteCmd = cli.Command{
	Name:  "execute",
	Short: "Execute CDS workflow template",
	Ctx: []cli.Arg{
		{Name: _ProjectKey},
	},
	Args: []cli.Arg{
		{Name: "template-id"},
		{Name: "name"},
	},
	Flags: []cli.Flag{
		{
			Kind:      reflect.Slice,
			Name:      "params",
			ShortHand: "p",
			Usage:     "Specify params for template",
			Default:   "",
		},
		{
			Kind:      reflect.Bool,
			Name:      "ignore-prompt",
			ShortHand: "i",
			Usage:     "Set to not ask interactively for params",
		},
		{
			Kind:      reflect.String,
			Name:      "output-dir",
			ShortHand: "d",
			Usage:     "Output directory",
			Default:   ".cds",
		},
		{
			Kind:    reflect.Bool,
			Name:    "force",
			Usage:   "Force, may override files",
			Default: "false",
		},
		{
			Kind:    reflect.Bool,
			Name:    "quiet",
			Usage:   "If true, do not output filename created",
			Default: "false",
		},
	},
}

func templateExecuteRun(v cli.Values) error {
	projectKey := v.GetString(_ProjectKey)

	// try to get the template from cds
	templateIDString := v.GetString("template-id")
	templateID, err := strconv.ParseInt(templateIDString, 10, 64)
	if err != nil {
		return fmt.Errorf("Invalid given template id")
	}
	wt, err := client.TemplateGet(templateID)
	if err != nil {
		return err
	}

	// init params from cli flags
	paramPairs := v.GetStringSlice("params")
	params := map[string]string{}
	for _, p := range paramPairs {
		if p != "" { // FIXME when no params given GetStringSlice returns one empty string
			ps := strings.Split(p, "=")
			if len(ps) < 2 {
				return fmt.Errorf("Invalid given param %s", ps[0])
			}
			params[ps[0]] = strings.Join(ps[1:], "=")
		}
	}

	// for parameters not given with flags, ask interactively if not disabled
	if !v.GetBool("ignore-prompt") {
		for _, p := range wt.Parameters {
			if _, ok := params[p.Key]; !ok {
				fmt.Printf("Value for param %s (type: %s, required: %t): ", p.Key, p.Type, p.Required)
				v, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				params[p.Key] = strings.TrimSuffix(v, "\n")
			}
		}
	}

	dir := strings.TrimSpace(v.GetString("output-dir"))
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, os.FileMode(0744)); err != nil {
		return fmt.Errorf("Unable to create directory %s: %v", v.GetString("output-dir"), err)
	}

	// check request before submit
	req := sdk.WorkflowTemplateRequest{
		Name:       v.GetString("name"),
		Parameters: params,
	}
	if err := wt.CheckParams(req); err != nil {
		return err
	}

	tr, err := client.TemplateExecute(projectKey, templateID, req)
	if err != nil {
		return err
	}

	return workflowTarReaderToFiles(dir, tr, v.GetBool("force"), v.GetBool("quiet"))
}

var templateUpdateCmd = cli.Command{
	Name:  "update",
	Short: "Update CDS workflow with new template",
	Ctx: []cli.Arg{
		{Name: _ProjectKey},
		{Name: _WorkflowName},
	},
	Flags: []cli.Flag{
		{
			Kind:      reflect.Slice,
			Name:      "params",
			ShortHand: "p",
			Usage:     "Specify params for template",
			Default:   "",
		},
		{
			Kind:      reflect.Bool,
			Name:      "ignore-prompt",
			ShortHand: "i",
			Usage:     "Set to not ask interactively for params",
		},
	},
}

func templateUpdateRun(v cli.Values) error {
	projectKey := v.GetString(_ProjectKey)
	workflowName := v.GetString(_WorkflowName)

	// try to get the workflow instance from cds
	wti, err := client.WorkflowTemplateInstanceGet(projectKey, workflowName)
	if err != nil {
		if sdk.ErrorIs(err, sdk.ErrNotFound) {
			return fmt.Errorf("The given workflow was not generated by a template")
		}
		return err
	}

	// try to get the workflow template from cds
	wt, err := client.TemplateGet(wti.WorkflowTemplateID)
	if err != nil {
		return err
	}

	// init old params from previous request
	old := map[string]string{}
	for _, p := range wt.Parameters {
		if v, ok := wti.Request.Parameters[p.Key]; ok {
			old[p.Key] = v
		}
	}

	// init params from cli flags
	paramPairs := v.GetStringSlice("params")
	params := map[string]string{}
	for _, p := range paramPairs {
		ps := strings.Split(p, "=")
		if len(ps) < 2 {
			return fmt.Errorf("Invalid given param %s", ps[0])
		}
		params[ps[0]] = strings.Join(ps[1:], "=")
	}

	// for parameters not given with flags, ask interactively if not disabled
	if !v.GetBool("ignore-prompt") {
		for _, p := range wt.Parameters {
			if _, ok := params[p.Key]; !ok {
				var oldValue string
				if o, ok := old[p.Key]; ok {
					oldValue = fmt.Sprintf(", old: %s", o)
				}
				fmt.Printf("Value for param %s (type: %s, required: %t%s): ", p.Key, p.Type, p.Required, oldValue)
				v, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				params[p.Key] = strings.TrimSuffix(v, "\n")
			}
		}
	}

	// check request before submit
	req := sdk.WorkflowTemplateRequest{
		Name:       wti.Request.Name,
		Parameters: params,
	}
	if err := wt.CheckParams(req); err != nil {
		return err
	}

	res, err := client.TemplateUpdate(projectKey, workflowName, req)
	if err != nil {
		return err
	}

	for _, r := range res {
		fmt.Println(r)
	}

	return nil
}
