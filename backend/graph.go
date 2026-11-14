package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file turns one child's subgraph in ArcadeDB into the laid-out node and edge
// lists that web/graph.html draws. The page places nodes on a fixed grid: one
// column per national stage (A..F, S for university, K for careers) and one row
// per lane. The constants below mirror COL/Y in graph.html.

var stageColumns = []struct{ Stage, Col, Node string }{
	{"national_a", "A", "fA"}, {"national_b", "B", "fB"}, {"national_c", "C", "fC"},
	{"national_d", "D", "fD"}, {"national_e", "E", "fE"}, {"national_f", "F", "fF"},
	{"national_university", "S", "s1"},
}

// schoolColumns are the columns a school-age item can sit in, oldest first.
var schoolColumns = []string{"A", "B", "C", "D", "E", "F"}

const (
	yStage  = 96
	ySchool = 150
	yMajor  = 306
	yLane1  = 478 // interests, competitions, methods
	yLane2  = 550 // sports
	yObs    = 640
)

// domainRows places each competency domain on its own row.
var domainRows = map[string]int{"numeracy": 222, "literacy": 278, "science": 334, "computing": 390}

// domainLabels must match DOMAIN_OF in graph.html, which maps them to talent areas.
var domainLabels = map[string]string{"numeracy": "Numerasi", "literacy": "Literasi", "science": "Sains", "computing": "Komputasi"}

var levelLabels = map[string]string{"preschool": "Belum sekolah", "primary": "SD", "junior": "SMP", "senior": "SMA", "university": "Kuliah"}

var schoolTypeLabels = map[string]string{"public": "Negeri", "private": "Swasta", "international": "Internasional"}

var tierLabels = map[string]string{"local": "lokal", "provincial": "provinsi", "national": "nasional", "international": "internasional"}

// observationSources maps what a guardian picks in the form to the weight class the page uses.
var observationSources = map[string]string{
	"Rapor": "report", "Catatan guru": "teacher", "Pengamatan": "parent", "Tes atau lomba": "test", "onboarding": "parent",
}

// GraphChild is the child's own record.
type GraphChild struct {
	ID            string  `json:"id"`
	Nickname      string  `json:"nickname"`
	Level         string  `json:"level"`
	Grade         int     `json:"grade"`
	BirthDate     string  `json:"birthDate"`
	WeeklyHourCap float64 `json:"weeklyHourCap"`
}

// GraphData is everything read from the database for one child.
type GraphData struct {
	Child         GraphChild
	Competencies  []Row // id, name, domain, stage
	Prerequisites []Row // from, to
	Masters       []Row // id, level, enjoyment, asOf, source
	School        []Row // id, name, type, curriculum, startYear
	Interests     []Row // id, name, score, asOf
	Sports        []Row // id, name, competitive, level, enjoyment, hoursPerWeek, status, asOf
	Methods       []Row // id, name, subject
	Competitions  []Row // id, name, type, year, tier, result
	Majors        []Row // id, name
	Observations  []Row // id, date, source, level, enjoyment, note, rel, target
}

// GraphNode is one node in the page's shape: add(id,t,c,y,n,s,st,step,lv,p,g).
type GraphNode struct {
	ID     string         `json:"id"`
	Type   string         `json:"t"`
	Col    string         `json:"c"`
	Y      int            `json:"y"`
	Name   string         `json:"n"`
	Sub    string         `json:"s"`
	Status string         `json:"st"`
	Step   int            `json:"step"`
	Level  *int           `json:"lv"`
	Props  map[string]any `json:"p"`
	Domain string         `json:"dom,omitempty"`
	Evid   int            `json:"ev,omitempty"`
}

// GraphMeta carries evidence details: meta(id,enjoy,src,hist,hours,cost).
type GraphMeta struct {
	ID      string   `json:"id"`
	Enjoy   int      `json:"enjoy"`
	Src     string   `json:"src"`
	History [][2]any `json:"hist"`
	Hours   *float64 `json:"hours,omitempty"`
	Cost    float64  `json:"cost"`
}

// GraphEdge is link(f,t,rel,color,step,route,props).
type GraphEdge struct {
	From  string         `json:"f"`
	To    string         `json:"t"`
	Rel   string         `json:"rel"`
	Color string         `json:"k"`
	Step  int            `json:"step"`
	Route string         `json:"route,omitempty"`
	Props map[string]any `json:"p,omitempty"`
}

// GraphView is the JSON the page renders.
type GraphView struct {
	Child   GraphChild  `json:"child"`
	NowCol  string      `json:"nowCol"`
	NowText string      `json:"nowText"`
	Nodes   []GraphNode `json:"nodes"`
	Meta    []GraphMeta `json:"meta"`
	Edges   []GraphEdge `json:"edges"`
	Hidden  int         `json:"hidden"` // items that did not fit their lane
}

func str(r Row, k string) string {
	switch v := r[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func num(r Row, k string) float64 {
	switch v := r[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		var f float64
		_, _ = fmt.Sscanf(v, "%g", &f)
		return f
	}
	return 0
}

func intp(v int) *int { return &v }

// timeline converts between calendar years and stage columns for one child.
type timeline struct {
	grade1Start int // calendar year in which grade 1 starts (July)
	current     int // index into stageColumns, -1 before school
}

func newTimeline(c GraphChild, today time.Time) timeline {
	schoolYear := today.Year()
	if today.Month() < time.July {
		schoolYear--
	}
	t := timeline{current: -1}
	switch {
	case c.Level == "university":
		t.grade1Start = schoolYear - 12
		t.current = len(stageColumns) - 1
	case c.Grade >= 1 && c.Grade <= 12:
		t.grade1Start = schoolYear - (c.Grade - 1)
		for i, s := range nationalStages[:6] {
			if c.Grade >= s.GradeFrom && c.Grade <= s.GradeTo {
				t.current = i
			}
		}
	default: // preschool: grade 1 in the year the child turns 7 by July
		if b, err := time.Parse("2006-01-02", c.BirthDate); err == nil {
			t.grade1Start = b.Year() + 7
			if b.Month() > time.July {
				t.grade1Start++
			}
		} else {
			t.grade1Start = schoolYear + 1
		}
	}
	return t
}

func (t timeline) status(col int) string {
	switch {
	case t.current < 0 || col > t.current:
		return "planned"
	case col == t.current:
		return "active"
	}
	return "done"
}

// columnOfYear returns the stage column index (0..5) of a calendar year.
func (t timeline) columnOfYear(year int) int {
	grade := year - t.grade1Start + 1
	for i, s := range nationalStages[:6] {
		if grade <= s.GradeTo {
			if grade < s.GradeFrom && i == 0 {
				return 0
			}
			return i
		}
	}
	return 5
}

// columnOfDate is columnOfYear for a YYYY-MM-DD date, using the school year.
func (t timeline) columnOfDate(date string) int {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return t.nowSlot()
	}
	y := d.Year()
	if d.Month() < time.July {
		y--
	}
	return t.columnOfYear(y)
}

// nowSlot is the school column items without a date belong to.
func (t timeline) nowSlot() int {
	if t.current < 0 {
		return 0
	}
	if t.current > 5 {
		return 5
	}
	return t.current
}

// lane hands out one column per item, nearest to the wanted one, preferring earlier columns.
type lane struct{ used [6]bool }

func (l *lane) take(want int) (int, bool) {
	for d := 0; d < 6; d++ {
		for _, c := range []int{want - d, want + d} {
			if c >= 0 && c < 6 && !l.used[c] {
				l.used[c] = true
				return c, true
			}
		}
	}
	return 0, false
}

// BuildGraph lays out a child's data for graph.html. It is pure, so it is unit tested.
func BuildGraph(d GraphData, today time.Time) GraphView {
	tl := newTimeline(d.Child, today)
	v := GraphView{Child: d.Child, Nodes: []GraphNode{}, Meta: []GraphMeta{}, Edges: []GraphEdge{}}
	if tl.current >= 0 {
		v.NowCol = stageColumns[tl.current].Col
	}
	if d.Child.Grade > 0 {
		v.NowText = fmt.Sprintf("Sekarang: kelas %d", d.Child.Grade)
	} else if d.Child.Level == "university" {
		v.NowText = "Sekarang: kuliah"
	}
	nodes := map[string]bool{}
	addNode := func(n GraphNode) {
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		nodes[n.ID] = true
		v.Nodes = append(v.Nodes, n)
	}
	link := func(e GraphEdge) {
		if nodes[e.From] && nodes[e.To] {
			v.Edges = append(v.Edges, e)
		}
	}

	// 1. Time axis.
	for i, s := range stageColumns {
		seed := nationalStages[i]
		from, to := tl.grade1Start+seed.GradeFrom-1, tl.grade1Start+seed.GradeTo
		sub := map[string]string{"primary": "SD", "junior": "SMP", "senior": "SMA"}[seed.Level]
		if seed.Level == "university" {
			sub = "Semester 1–8"
		} else {
			sub += fmt.Sprintf(" kelas %d–%d", seed.GradeFrom, seed.GradeTo)
			if seed.GradeFrom == seed.GradeTo {
				sub = fmt.Sprintf("%s kelas %d", strings.Fields(sub)[0], seed.GradeFrom)
			}
		}
		p := map[string]any{"years": fmt.Sprintf("%d–%d", from, to)}
		if i == tl.current && d.Child.Grade > 0 {
			p["position"] = fmt.Sprintf("kelas %d", d.Child.Grade)
		}
		addNode(GraphNode{ID: s.Node, Type: "stage", Col: s.Col, Y: yStage, Name: seed.Name, Sub: sub, Status: tl.status(i), Step: 1, Props: p})
		if i > 0 {
			link(GraphEdge{From: stageColumns[i-1].Node, To: s.Node, Rel: "NEXT", Color: "ink", Step: 1})
		}
	}

	// School: the one the child attends now, under the current stage.
	if len(d.School) > 0 && tl.current >= 0 {
		s := d.School[0]
		parts := []string{}
		if t := schoolTypeLabels[str(s, "type")]; t != "" {
			parts = append(parts, t)
		}
		if c := str(s, "curriculum"); c != "" {
			parts = append(parts, c)
		}
		p := map[string]any{"type": schoolTypeLabels[str(s, "type")], "curriculum": str(s, "curriculum")}
		if y := num(s, "startYear"); y > 0 {
			p["since"] = int(y)
		}
		addNode(GraphNode{ID: str(s, "id"), Type: "school", Col: stageColumns[tl.current].Col, Y: ySchool,
			Name: str(s, "name"), Sub: strings.Join(parts, " · "), Status: "active", Step: 1, Props: p})
	}

	// 2. Competencies from the catalog, with the child's level where known.
	stageIndex := map[string]int{}
	for i, s := range stageColumns {
		stageIndex[s.Stage] = i
	}
	masters := map[string]Row{}
	for _, m := range d.Masters {
		masters[str(m, "id")] = m
	}
	history := map[string][][2]any{}
	obsSorted := append([]Row(nil), d.Observations...)
	sort.SliceStable(obsSorted, func(i, j int) bool { return str(obsSorted[i], "date") < str(obsSorted[j], "date") })
	for _, o := range obsSorted {
		if lv := num(o, "level"); lv > 0 {
			history[str(o, "target")] = append(history[str(o, "target")], [2]any{str(o, "date"), int(lv)})
		}
	}
	for _, k := range d.Competencies {
		col, ok := stageIndex[str(k, "stage")]
		row, okRow := domainRows[str(k, "domain")]
		if !ok || !okRow || col > 5 {
			continue
		}
		id := str(k, "id")
		n := GraphNode{ID: id, Type: "comp", Col: stageColumns[col].Col, Y: row, Name: str(k, "name"),
			Sub: domainLabels[str(k, "domain")], Status: tl.status(col), Step: 2, Level: intp(0), Props: map[string]any{}}
		meta := GraphMeta{ID: id, Src: "parent", History: history[id]}
		if m, ok := masters[id]; ok {
			n.Level = intp(int(num(m, "level")))
			n.Props["asOf"] = str(m, "asOf")
			meta.Enjoy = int(num(m, "enjoyment"))
			if s := observationSources[str(m, "source")]; s != "" {
				meta.Src = s
			}
		}
		addNode(n)
		v.Meta = append(v.Meta, meta)
	}
	for _, p := range d.Prerequisites {
		link(GraphEdge{From: str(p, "from"), To: str(p, "to"), Rel: "PREREQUISITE_OF", Color: "blue", Step: 2})
	}

	// 3. The child: interests, competitions and methods share lane 1, sports lane 2.
	var lane1, lane2 lane
	now := tl.nowSlot()
	place := func(l *lane, want int) (string, bool) {
		c, ok := l.take(want)
		if !ok {
			v.Hidden++
			return "", false
		}
		return schoolColumns[c], true
	}
	comps := append([]Row(nil), d.Competitions...)
	sort.SliceStable(comps, func(i, j int) bool { return num(comps[i], "year") < num(comps[j], "year") })
	for _, c := range comps {
		year := int(num(c, "year"))
		col, ok := place(&lane1, tl.columnOfYear(year))
		if !ok {
			continue
		}
		tier := tierLabels[str(c, "tier")]
		sub := strings.ToUpper(str(c, "type"))
		if str(c, "type") == "general" || str(c, "type") == "" {
			sub = "Umum"
		}
		if r := str(c, "result"); r != "" {
			sub += " · " + r
		}
		id := str(c, "id") + fmt.Sprintf("_%d", year)
		addNode(GraphNode{ID: id, Type: "contest", Col: col, Y: yLane1, Name: str(c, "name"), Sub: sub, Status: "done", Step: 3,
			Props: map[string]any{"type": str(c, "type"), "tier": tier, "result": str(c, "result"), "year": fmt.Sprint(year)}})
		v.Meta = append(v.Meta, GraphMeta{ID: id, Src: "contest", History: [][2]any{}})
	}
	for _, it := range d.Interests {
		col, ok := place(&lane1, now)
		if !ok {
			continue
		}
		id := str(it, "id")
		score := int(num(it, "score"))
		addNode(GraphNode{ID: id, Type: "interest", Col: col, Y: yLane1, Name: str(it, "name"), Sub: "Minat",
			Status: "active", Step: 3, Level: intp(score), Props: map[string]any{"asOf": str(it, "asOf")}})
		v.Meta = append(v.Meta, GraphMeta{ID: id, Enjoy: score, Src: "parent", History: history[id]})
	}
	for _, m := range d.Methods {
		col, ok := place(&lane1, now)
		if !ok {
			continue
		}
		id := str(m, "id")
		addNode(GraphNode{ID: id, Type: "method", Col: col, Y: yLane1, Name: str(m, "name"), Sub: "Metode belajar",
			Status: "active", Step: 3, Props: map[string]any{"subject": domainLabels[str(m, "subject")]}})
		if row := str(m, "subject"); row != "" {
			for _, k := range d.Competencies {
				if str(k, "domain") == row && stageIndex[str(k, "stage")] == now {
					link(GraphEdge{From: id, To: str(k, "id"), Rel: "TRAINS", Color: "blue", Step: 3})
				}
			}
		}
	}
	for _, s := range d.Sports {
		col, ok := place(&lane2, now)
		if !ok {
			continue
		}
		id := str(s, "id")
		status := "active"
		if str(s, "status") == "stopped" || str(s, "status") == "done" {
			status = "done"
		}
		competitive := "tidak"
		if b, _ := s["competitive"].(bool); b {
			competitive = "ya"
		}
		lv := int(num(s, "level"))
		hours := num(s, "hoursPerWeek")
		addNode(GraphNode{ID: id, Type: "sport", Col: col, Y: yLane2, Name: str(s, "name"), Sub: "Olahraga",
			Status: status, Step: 3, Level: intp(lv), Props: map[string]any{"competitive": competitive}, Domain: "kinesthetic"})
		v.Meta = append(v.Meta, GraphMeta{ID: id, Enjoy: int(num(s, "enjoyment")), Src: "parent", History: history[id], Hours: &hours})
	}

	// 4. Observations: the most recent ones, each in the column of its school year.
	var lane3 lane
	recent := append([]Row(nil), obsSorted...)
	sort.SliceStable(recent, func(i, j int) bool { return str(recent[i], "date") > str(recent[j], "date") })
	seen := map[string]bool{}
	for _, o := range recent {
		id := str(o, "id")
		if seen[id] {
			continue
		}
		seen[id] = true
		col, ok := place(&lane3, tl.columnOfDate(str(o, "date")))
		if !ok {
			continue
		}
		source := str(o, "source")
		name := source
		if source == "onboarding" {
			name = "Perkiraan awal"
		}
		p := map[string]any{"date": str(o, "date"), "source": name}
		if note := str(o, "note"); note != "" {
			p["note"] = note
		}
		if lv := num(o, "level"); lv > 0 {
			p["level"] = fmt.Sprint(int(lv))
		}
		addNode(GraphNode{ID: id, Type: "obs", Col: col, Y: yObs, Name: name, Sub: str(o, "date"), Status: "done", Step: 4, Props: p})
	}
	for _, o := range d.Observations {
		rel := str(o, "rel")
		link(GraphEdge{From: str(o, "id"), To: str(o, "target"), Rel: rel, Color: "amber", Step: 4, Route: "side"})
	}

	// 5. Goals: majors in the university column.
	majorRows := []int{yMajor, yLane1, 392, yLane2, 222}
	for i, m := range d.Majors {
		if i >= len(majorRows) {
			v.Hidden++
			continue
		}
		addNode(GraphNode{ID: str(m, "id"), Type: "major", Col: "S", Y: majorRows[i], Name: str(m, "name"), Sub: "Jurusan S1", Status: "planned", Step: 5})
	}
	for i := range v.Meta {
		if v.Meta[i].History == nil {
			v.Meta[i].History = [][2]any{}
		}
	}
	return v
}

// errNotFound means the child does not exist or is not the signed-in user's.
var errNotFound = errors.New("not found")

// LoadGraph reads one child's subgraph. The guardian check is part of every query.
func LoadGraph(ctx context.Context, db *DB, userID, childID string) (GraphData, error) {
	var d GraphData
	params := map[string]any{"userId": userID, "childId": childID}
	rows, err := db.Run(ctx, Cypher(`MATCH (u:User {id: $userId})-[:GUARDIAN_OF]->(c:Child {id: $childId})
RETURN c.id AS id, c.nickname AS nickname, c.level AS level, c.grade AS grade, c.birthDate AS birthDate, c.weeklyHourCap AS weeklyHourCap`, params), "")
	if err != nil {
		return d, err
	}
	if len(rows) == 0 {
		return d, errNotFound
	}
	c := rows[0]
	d.Child = GraphChild{ID: str(c, "id"), Nickname: str(c, "nickname"), Level: str(c, "level"), Grade: int(num(c, "grade")),
		BirthDate: str(c, "birthDate"), WeeklyHourCap: num(c, "weeklyHourCap")}

	queries := []struct {
		into  *[]Row
		query string
	}{
		{&d.Competencies, `MATCH (k:Competency)-[:TAUGHT_IN]->(s:Stage) RETURN k.id AS id, k.name AS name, k.domain AS domain, s.id AS stage`},
		{&d.Prerequisites, `MATCH (a:Competency)-[:PREREQUISITE_OF]->(b:Competency) RETURN a.id AS from, b.id AS to`},
		{&d.Masters, `MATCH (c:Child {id: $childId})-[m:MASTERS]->(k:Competency)
RETURN k.id AS id, m.level AS level, m.enjoyment AS enjoyment, m.asOf AS asOf, m.source AS source`},
		{&d.School, `MATCH (c:Child {id: $childId})-[a:ATTENDS]->(s:School)
OPTIONAL MATCH (s)-[:USES]->(k:Curriculum)
RETURN s.id AS id, s.name AS name, s.type AS type, k.name AS curriculum, a.startYear AS startYear`},
		{&d.Interests, `MATCH (c:Child {id: $childId})-[r:INTERESTED_IN]->(i:Interest) RETURN i.id AS id, i.name AS name, r.score AS score, r.asOf AS asOf`},
		{&d.Sports, `MATCH (c:Child {id: $childId})-[r:PRACTICES]->(s:Sport)
RETURN s.id AS id, s.name AS name, s.competitive AS competitive, r.level AS level, r.enjoyment AS enjoyment,
  r.hoursPerWeek AS hoursPerWeek, r.status AS status, r.asOf AS asOf`},
		{&d.Methods, `MATCH (c:Child {id: $childId})-[:LEARNS_WITH]->(m:Method) RETURN m.id AS id, m.name AS name, m.subject AS subject`},
		{&d.Competitions, `MATCH (c:Child {id: $childId})-[p:PARTICIPATES_IN]->(r:Round)<-[:HAS_ROUND]-(e:CompetitionEdition)<-[:HAS_EDITION]-(k:Competition)
RETURN k.id AS id, k.name AS name, k.type AS type, e.year AS year, r.tier AS tier, p.result AS result`},
		{&d.Majors, `MATCH (c:Child {id: $childId})-[:AIMS_FOR]->(m:Major) RETURN m.id AS id, m.name AS name`},
		{&d.Observations, `MATCH (o:Observation)-[:ABOUT]->(c:Child {id: $childId})
MATCH (o)-[r]->(x) WHERE type(r) <> 'ABOUT'
RETURN o.id AS id, o.date AS date, o.source AS source, o.level AS level, o.enjoyment AS enjoyment, o.note AS note,
  type(r) AS rel, x.id AS target`},
	}
	for _, q := range queries {
		rows, err := db.Run(ctx, Cypher(q.query, params), "")
		if err != nil {
			return d, err
		}
		*q.into = rows
	}
	return d, nil
}

// ObservationInput is what the "Input berkala" form sends.
type ObservationInput struct {
	Kind      string `json:"kind"`   // comp | interest | sport
	Target    string `json:"target"` // node id
	Source    string `json:"source"` // Rapor | Catatan guru | Pengamatan | Tes atau lomba
	Level     int    `json:"level"`  // 1-4
	Enjoyment int    `json:"enjoyment"`
	Note      string `json:"note"`
}

var observationKinds = map[string]struct{ Label, Measure, Relation, Field string }{
	"comp":     {"Competency", "MEASURES", "MASTERS", "level"},
	"interest": {"Interest", "REVEALS", "INTERESTED_IN", "score"},
	"sport":    {"Sport", "MEASURES", "PRACTICES", "level"},
}

// Validate checks the form input.
func (in *ObservationInput) Validate() error {
	in.Note = strings.TrimSpace(in.Note)
	switch {
	case observationKinds[in.Kind].Label == "":
		return errors.New("kind must be comp, interest or sport")
	case in.Target == "" || len(in.Target) > 120:
		return errors.New("target is required")
	case in.Source == "onboarding" || observationSources[in.Source] == "":
		return errors.New("source is not valid")
	case in.Level < 1 || in.Level > 4:
		return errors.New("level must be 1 to 4")
	case in.Enjoyment < 0 || in.Enjoyment > 4:
		return errors.New("enjoyment must be 0 to 4")
	case len([]rune(in.Note)) > 500:
		return errors.New("note is too long")
	}
	return nil
}

// Statements records the observation and moves the child's current level on that node.
// Labels and relation names come from the fixed table above, never from the request.
func (in *ObservationInput) Statements(userID, childID, observationID string, today time.Time) []Statement {
	k := observationKinds[in.Kind]
	params := map[string]any{"userId": userID, "childId": childID, "target": in.Target, "observationId": observationID,
		"today": today.Format("2006-01-02"), "source": in.Source, "level": in.Level, "note": in.Note}
	set := ""
	if in.Enjoyment > 0 {
		params["enjoyment"] = in.Enjoyment
		set = ", m.enjoyment = $enjoyment, o.enjoyment = $enjoyment"
	}
	return []Statement{Cypher(`MATCH (u:User {id: $userId})-[:GUARDIAN_OF]->(c:Child {id: $childId}), (t:`+k.Label+` {id: $target})
CREATE (o:Observation {id: $observationId, date: $today, source: $source, level: $level, note: $note})
CREATE (o)-[:ABOUT]->(c)
CREATE (o)-[:`+k.Measure+`]->(t)
MERGE (c)-[m:`+k.Relation+`]->(t)
SET m.`+k.Field+` = $level, m.asOf = $today, m.source = $source`+set+`
RETURN o.id AS id`, params)}
}
