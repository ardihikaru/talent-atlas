package main

import (
	"strings"
	"testing"
	"time"
)

func catalogRows() (comps, prereqs []Row) {
	for _, d := range competencyDomains {
		for i, s := range nationalStages[:6] {
			id := d.ID + "_" + s.ID
			comps = append(comps, Row{"id": id, "name": competencyNames[d.ID][i], "domain": d.ID, "stage": s.ID})
			if i > 0 {
				prereqs = append(prereqs, Row{"from": d.ID + "_" + nationalStages[i-1].ID, "to": id})
			}
		}
	}
	return
}

func sampleGraphData() GraphData {
	comps, prereqs := catalogRows()
	return GraphData{
		Child:         GraphChild{ID: "child_1", Nickname: "Raka", Level: "junior", Grade: 8, BirthDate: "2012-09-03", WeeklyHourCap: 12},
		Competencies:  comps,
		Prerequisites: prereqs,
		Masters:       []Row{{"id": "numeracy_national_d", "level": 2.0, "enjoyment": 2.0, "asOf": "2026-11-14", "source": "onboarding"}},
		School:        []Row{{"id": "school_smp", "name": "SMP Contoh", "type": "private", "curriculum": "Cambridge", "startYear": 2025.0}},
		Interests:     []Row{{"id": "interest_robotika", "name": "Robotika", "score": 4.0}},
		Sports: []Row{
			{"id": "swimming", "name": "Renang", "competitive": true, "level": 3.0, "enjoyment": 3.0, "hoursPerWeek": 2.0},
			{"id": "aikido", "name": "Aikido", "competitive": false, "level": 3.0, "hoursPerWeek": 3.0},
		},
		Methods:      []Row{{"id": "abacus", "name": "Sempoa", "subject": "numeracy"}},
		Competitions: []Row{{"id": "competition_poster", "name": "Lomba Poster", "type": "general", "year": 2023.0, "tier": "local", "result": "Juara 2"}},
		Majors:       []Row{{"id": "major_informatika", "name": "Informatika"}},
		Observations: []Row{
			{"id": "obs_1", "date": "2026-11-14", "source": "onboarding", "level": 2.0, "rel": "MEASURES", "target": "numeracy_national_d"},
			{"id": "obs_2", "date": "2026-11-20", "source": "Rapor", "level": 3.0, "note": "naik", "rel": "MEASURES", "target": "numeracy_national_d"},
		},
	}
}

func nodeByID(v GraphView, id string) *GraphNode {
	for i := range v.Nodes {
		if v.Nodes[i].ID == id {
			return &v.Nodes[i]
		}
	}
	return nil
}

func TestBuildGraphLayout(t *testing.T) {
	today := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	v := BuildGraph(sampleGraphData(), today)

	if v.NowCol != "D" || v.NowText != "Sekarang: kelas 8" {
		t.Fatalf("now: %q %q", v.NowCol, v.NowText)
	}
	// Grade 8 in school year 2026 means grade 1 started in 2019: Fase A is 2019–2021.
	if fa := nodeByID(v, "fA"); fa == nil || fa.Props["years"] != "2019–2021" || fa.Status != "done" {
		t.Fatalf("fA: %+v", fa)
	}
	if fd := nodeByID(v, "fD"); fd.Status != "active" || fd.Props["position"] != "kelas 8" {
		t.Fatalf("fD: %+v", fd)
	}
	if s1 := nodeByID(v, "s1"); s1.Status != "planned" {
		t.Fatalf("s1: %+v", s1)
	}
	// All 24 catalog competencies, status from the timeline, level from MASTERS.
	alg := nodeByID(v, "numeracy_national_d")
	if alg == nil || alg.Col != "D" || alg.Y != 222 || *alg.Level != 2 || alg.Status != "active" || alg.Name != "Aljabar" || alg.Sub != "Numerasi" {
		t.Fatalf("algebra: %+v", alg)
	}
	if k := nodeByID(v, "computing_national_f"); k.Status != "planned" || *k.Level != 0 {
		t.Fatalf("planned comp: %+v", k)
	}
	// The poster contest of 2023 (grade 5) sits in Fase C.
	if c := nodeByID(v, "competition_poster_2023"); c == nil || c.Col != "C" || c.Sub != "Umum · Juara 2" {
		t.Fatalf("contest: %+v", c)
	}
	// Interest and method share lane 1 without overlapping each other or the contest.
	cols := map[string]string{}
	for _, n := range v.Nodes {
		if n.Y == yLane1 && n.Col != "S" {
			if other, dup := cols[n.Col]; dup {
				t.Fatalf("lane 1 overlap in %s: %s and %s", n.Col, other, n.ID)
			}
			cols[n.Col] = n.ID
		}
	}
	if len(cols) != 3 {
		t.Fatalf("lane 1: %v", cols)
	}
	if sw := nodeByID(v, "swimming"); sw.Domain != "kinesthetic" || sw.Props["competitive"] != "ya" {
		t.Fatalf("sport: %+v", sw)
	}
	if m := nodeByID(v, "major_informatika"); m.Col != "S" {
		t.Fatalf("major: %+v", m)
	}
	// History comes from observations, oldest first, and the meta source from MASTERS.
	for _, m := range v.Meta {
		if m.ID == "numeracy_national_d" {
			if len(m.History) != 2 || m.History[1][1] != 3 || m.Enjoy != 2 {
				t.Fatalf("meta: %+v", m)
			}
		}
	}
	// Every edge points at drawn nodes, and the expected kinds are present.
	rels := map[string]int{}
	for _, e := range v.Edges {
		if nodeByID(v, e.From) == nil || nodeByID(v, e.To) == nil {
			t.Fatalf("dangling edge %+v", e)
		}
		rels[e.Rel]++
	}
	if rels["NEXT"] != 6 || rels["PREREQUISITE_OF"] != 20 || rels["MEASURES"] != 2 || rels["TRAINS"] != 1 {
		t.Fatalf("edges: %v", rels)
	}
}

func TestBuildGraphOverflowAndPreschool(t *testing.T) {
	d := sampleGraphData()
	d.Child = GraphChild{ID: "c", Nickname: "Kecil", Level: "preschool", Grade: 0, BirthDate: "2021-03-01"}
	d.School = nil
	for i := 0; i < 9; i++ {
		d.Sports = append(d.Sports, Row{"id": "sport_" + string(rune('a'+i)), "name": "x", "level": 1.0})
	}
	v := BuildGraph(d, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC))
	if v.NowCol != "" || v.NowText != "" {
		t.Fatalf("preschool now: %q %q", v.NowCol, v.NowText)
	}
	if fa := nodeByID(v, "fA"); fa.Status != "planned" || !strings.HasPrefix(fa.Props["years"].(string), "2028") {
		t.Fatalf("preschool fA: %+v", fa)
	}
	if v.Hidden != 5 { // 11 sports, 6 columns
		t.Fatalf("hidden = %d", v.Hidden)
	}
}

func TestObservationInput(t *testing.T) {
	ok := ObservationInput{Kind: "comp", Target: "numeracy_national_d", Source: "Rapor", Level: 3, Enjoyment: 2, Note: " lancar "}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	st := ok.Statements("u", "c", "o", time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC))[0]
	for _, want := range []string{"(t:Competency {id: $target})", "[:MEASURES]", "[m:MASTERS]", "m.level = $level", "m.enjoyment"} {
		if !strings.Contains(st.Command, want) {
			t.Errorf("statement lacks %q:\n%s", want, st.Command)
		}
	}
	if st.Params["note"] != "lancar" {
		t.Errorf("note not trimmed: %q", st.Params["note"])
	}
	for _, bad := range []ObservationInput{
		{Kind: "Child) DETACH DELETE (x", Target: "a", Source: "Rapor", Level: 1},
		{Kind: "comp", Target: "a", Source: "onboarding", Level: 1},
		{Kind: "comp", Target: "a", Source: "Rapor", Level: 5},
		{Kind: "sport", Target: "", Source: "Rapor", Level: 1},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestCatalogIDsMatchSeed(t *testing.T) {
	cases := map[string]string{"Renang": "swimming", " badminton ": "badminton", "Sempoa": "abacus", "Panahan": "panahan"}
	for name, want := range cases {
		catalog := sportCatalog
		if name == "Sempoa" {
			catalog = methodCatalog
		}
		if got := catalogID(name, catalog); got != want {
			t.Errorf("%q: got %q want %q", name, got, want)
		}
	}
}
