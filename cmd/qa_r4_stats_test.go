package cmd

// r4StatsCases: voice calls stats against the API's wire format.
func r4StatsCases() []qaCase {
	const calls = `{"api_id": "qa", "meta": {"limit": 20, "offset": 0, "next": null}, "objects": [` +
		`{"call_uuid": "` + placeholderUUID + `", "call_direction": "outbound", "from_number": "+14155550100", "to_number": "14155550123", ` +
		`"answer_time": null, "bill_duration": 0, "hangup_cause_name": "No Answer"}, ` +
		`{"call_uuid": "` + r2LiveCall + `", "call_direction": "outbound", "from_number": "14155550100", "to_number": "14155550123", ` +
		`"answer_time": "2026-11-10 10:00:05+00:00", "bill_duration": 30}]}`
	const window = "&end_time__gte=2026-11-01+00%3A00%3A00&end_time__lte=2026-11-02+23%3A59%3A59"
	return []qaCase{{
		id: "r4/stats-number", title: "--number reads its outbound and inbound calls, and a call both reads return counts once",
		args:    qaArgs("voice calls stats --number +14155550100 --since 2026-11-01 --until 2026-11-02 -o json"),
		replies: map[string]string{"GET Call/*": calls},
		setup:   statsAt,
		sent: []string{
			"GET Call/?call_direction=outbound" + window + "&from_number=%2B14155550100&limit=20&offset=0",
			"GET Call/?call_direction=inbound" + window + "&limit=20&offset=0&to_number=%2B14155550100",
		},
		stdout: []string{`"calls_counted": 2`, `"duplicates_dropped": 2`, `"connect_rate_pct": 50`},
	}}
}
