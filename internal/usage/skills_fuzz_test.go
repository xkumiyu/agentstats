package usage

import (
	"testing"
	"time"
)

func FuzzSkillDetectorsDoNotPanic(f *testing.F) {
	f.Add("$report")
	f.Add("<skill name=\"report\">body</skill>")
	f.Add("ordinary prose mentioning $report and SKILL.md")
	f.Fuzz(func(t *testing.T, text string) {
		DetectInjectedSkills(text, "s", "t", time.Time{}, SourceRef{})
		DetectExplicitRequest(text, "s", "t", time.Time{}, SourceRef{})
		DetectImplicitAccess(text, "s", "t", time.Time{}, SourceRef{})
		DetectStructuredSkillTool(ToolObservation{RawName: "skills.read", Arguments: text})
	})
}
