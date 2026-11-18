package cmd

// r4BugsCases are the v1.3.4 small-bugs fixes. scripts/smoke.sh holds --version
// before a global flag (TestMain registers it up front), skill_windows_test.go the junctions.
func r4BugsCases() []qaCase {
	return []qaCase{{
		id: "r4/bugs-verify", title: "verify sessions create demanded --app-uuid, which the API defaults to the account's default Verify application",
		args:   qaArgs("verify sessions create --recipient +14155550100 --yes"),
		sent:   []string{"POST Verify/Session/"},
		stdout: []string{`"data"`},
		check: func(r *qaResult) []string {
			return qaExpect(len(r.reqs) == 1 && r.reqs[0].Body["app_uuid"] == nil, "want one request without an app_uuid, got %v", r.reqs)
		},
	}}
}
