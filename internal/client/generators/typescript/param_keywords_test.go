package typescript

import "testing"

func TestParameterKeywords(t *testing.T) {
	generator := &RESTGenerator{}
	for _, keyword := range []string{"class", "default", "delete", "await", "interface"} {
		if got := generator.toTSParamName(keyword); got != keyword+"Param" {
			t.Errorf("parameter %q becomes %q", keyword, got)
		}
	}
	if got := generator.toTSParamName("asset_class"); got != "assetClass" {
		t.Errorf("ordinary parameter becomes %q", got)
	}
}
