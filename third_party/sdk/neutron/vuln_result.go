package neutron

import "github.com/chainreactors/sdk/pkg/types"

// VulnResult builds a serializable VulnResult from the execution result.
func (r *ExecuteResult) VulnResult(target string) *types.VulnResult {
	if r == nil {
		return nil
	}
	vr := &types.VulnResult{
		Target:  target,
		Matched: r.Matched(),
	}
	if tmpl := r.template; tmpl != nil {
		vr.TemplateID = tmpl.Id
		vr.TemplateName = tmpl.Info.Name
		vr.Severity = tmpl.Info.Severity
		vr.Description = tmpl.Info.Description
		vr.References = tmpl.Info.Reference
		vr.Tags = tmpl.Info.Tags
		vr.Classification = tmpl.Info.Classification
	}
	if data := r.data; data != nil {
		if op := data.Result; op != nil {
			vr.Matches = op.Matches
			vr.Extracts = op.Extracts
			vr.OutputExtracts = op.OutputExtracts
			vr.Request = op.Request
			vr.Response = op.Response
			vr.PayloadValues = op.PayloadValues
		}
		vr.Events = data.Events
	}
	return vr
}
