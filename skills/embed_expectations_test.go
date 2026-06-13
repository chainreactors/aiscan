package skills

var baseExpectedSkills = []string{"aiscan", "ioa", "scan", "gogo", "spray", "zombie", "neutron", "report", "search"}
var baseInternalSkills = []string{"scan", "gogo", "spray", "zombie", "neutron", "search"}

var extraExpected []string
var extraInternal []string

//nolint:unused // called from build-tagged test files
func addExpectedSkill(name string, internal bool) {
	extraExpected = append(extraExpected, name)
	if internal {
		extraInternal = append(extraInternal, name)
	}
}

func expectedEmbeddedSkillNames() []string {
	return append(append([]string(nil), baseExpectedSkills...), extraExpected...)
}

func internalPromptSkillNames() []string {
	return append(append([]string(nil), baseInternalSkills...), extraInternal...)
}
