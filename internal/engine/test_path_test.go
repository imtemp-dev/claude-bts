package engine

import "testing"

// IsTestPath had no .NET rule (trial problem #79, Multica AGEN-53).  On
// true-shark the Work unit tests, `api/Work/Tests/Work.Module.Tests.Unit/
// *Tests.cs`, read as production code to `bts interrogate prepare`, the
// interrogate gate and `falsifier mutate`; component tests passed only
// through the lowercase `tests` segment of their path.
func TestIsTestPath_DotNetProjects(t *testing.T) {
	tests := map[string]bool{
		"api/Work/Tests/Work.Module.Tests.Unit/GetWorkItemsQueryHandlerTests.cs": true,
		"api/Automations/Automations.Tests.Unit/Binder/BinderTests.cs":           true,
		"api/tests/tests.component/Features/Work/GetWorkItemsTests.cs":           true,
		"src/MyApp.UnitTests/Fixtures/Seed.cs":                                   true,
		"src/MyApp/Billing/InvoiceTests.cs":                                      true,

		"api/Work/Work.Module/Queries/GetWorkItemsQuery.cs":             false,
		"api/carepatron.api/Controllers/Practitioner/WorkController.cs": false,
		"src/Experiments/ABTest.cs":                                     false,
		"src/Contest/Contest.cs":                                        false,
		"src/Testimonials/Quote.cs":                                     false,
	}
	for p, want := range tests {
		if got := IsTestPath(p); got != want {
			t.Errorf("IsTestPath(%q) = %v, want %v", p, got, want)
		}
	}
}

// The rules that held before stay as they were.
func TestIsTestPath_KeepsEarlierRules(t *testing.T) {
	for _, p := range []string{
		"tests/test_relay.py", "pkg/x/x_test.go", "web/src/__tests__/a.ts",
		"web/src/a.test.ts", "web/src/a.spec.tsx", "conftest.py", "internal/testdata/a.json",
	} {
		if !IsTestPath(p) {
			t.Errorf("IsTestPath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"src/relay.py", "internal/engine/validator.go", "web/src/App.tsx"} {
		if IsTestPath(p) {
			t.Errorf("IsTestPath(%q) = true, want false", p)
		}
	}
}
