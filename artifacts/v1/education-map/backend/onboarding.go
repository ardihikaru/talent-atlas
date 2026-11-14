package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// OnboardingInput is what the wizard sends when the guardian confirms the review step.
type OnboardingInput struct {
	Child struct {
		Nickname  string `json:"nickname"`
		BirthDate string `json:"birthDate"` // YYYY-MM-DD
	} `json:"child"`
	Position struct {
		Level string `json:"level"` // preschool | primary | junior | senior | university
		Grade int    `json:"grade"` // 1-12, 0 when not in school
	} `json:"position"`
	School *struct {
		Name       string `json:"name"`
		Type       string `json:"type"`       // public | private | international
		Curriculum string `json:"curriculum"` // national | cambridge | ib | other
		StartYear  int    `json:"startYear"`
	} `json:"school"`
	Baseline []struct {
		Domain    string `json:"domain"`
		Level     int    `json:"level"`     // 1-4
		Enjoyment int    `json:"enjoyment"` // 0 = not answered
	} `json:"baseline"`
	Interests []struct {
		Name  string `json:"name"`
		Score int    `json:"score"` // 1-4
	} `json:"interests"`
	Sports []struct {
		Name         string  `json:"name"`
		Level        int     `json:"level"`
		Enjoyment    int     `json:"enjoyment"`
		HoursPerWeek float64 `json:"hoursPerWeek"`
	} `json:"sports"`
	Methods      []string `json:"methods"`
	Competitions []struct {
		Name   string `json:"name"`
		Type   string `json:"type"` // general | osn | o2sn | fls2n | other
		Year   int    `json:"year"`
		Tier   string `json:"tier"` // local | provincial | national | international
		Result string `json:"result"`
	} `json:"competitions"`
	Goals struct {
		WeeklyHourCap float64  `json:"weeklyHourCap"`
		Majors        []string `json:"majors"`
		AIConsent     bool     `json:"aiConsent"`
	} `json:"goals"`
}

var (
	levels      = map[string]bool{"preschool": true, "primary": true, "junior": true, "senior": true, "university": true}
	schoolTypes = map[string]bool{"public": true, "private": true, "international": true}
	curricula   = map[string]bool{"national": true, "cambridge": true, "ib": true, "other": true}
	domains     = map[string]bool{"numeracy": true, "literacy": true, "science": true, "computing": true}
	tiers       = map[string]int{"local": 1, "provincial": 2, "national": 3, "international": 4}
	compTypes   = map[string]bool{"general": true, "osn": true, "o2sn": true, "fls2n": true, "other": true}
	slugRe      = regexp.MustCompile(`[^a-z0-9]+`)
)

// Slug turns a display name into a stable id fragment.
func Slug(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "_"), "_")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// SuggestGrade proposes a school grade from the birth date: a child who is 7
// on 1 July of the running school year is expected in grade 1.
// It returns 0 for a child below school age and 13 for one past grade 12.
func SuggestGrade(birth, today time.Time) int {
	schoolYear := today.Year()
	if today.Month() < time.July {
		schoolYear--
	}
	cutoff := time.Date(schoolYear, time.July, 1, 0, 0, 0, 0, time.UTC)
	age := cutoff.Year() - birth.Year()
	if birth.Month() > cutoff.Month() || (birth.Month() == cutoff.Month() && birth.Day() > cutoff.Day()) {
		age--
	}
	grade := age - 6
	if grade < 1 {
		return 0
	}
	if grade > 12 {
		return 13
	}
	return grade
}

// Validate checks the input and normalises it. It returns every problem found.
func (in *OnboardingInput) Validate(today time.Time) error {
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	inRange := func(v, lo, hi int) bool { return v >= lo && v <= hi }

	in.Child.Nickname = strings.TrimSpace(in.Child.Nickname)
	if n := len([]rune(in.Child.Nickname)); n < 1 || n > 40 {
		bad("child.nickname must be 1 to 40 characters")
	}
	birth, err := time.Parse("2006-01-02", in.Child.BirthDate)
	if err != nil || birth.After(today) || today.Year()-birth.Year() > 30 {
		bad("child.birthDate must be a past date in YYYY-MM-DD format")
	}
	if !levels[in.Position.Level] {
		bad("position.level is not valid")
	}
	switch in.Position.Level {
	case "primary":
		if !inRange(in.Position.Grade, 1, 6) {
			bad("position.grade must be 1 to 6 for primary")
		}
	case "junior":
		if !inRange(in.Position.Grade, 7, 9) {
			bad("position.grade must be 7 to 9 for junior")
		}
	case "senior":
		if !inRange(in.Position.Grade, 10, 12) {
			bad("position.grade must be 10 to 12 for senior")
		}
	default:
		in.Position.Grade = 0
	}
	if in.School != nil {
		in.School.Name = strings.TrimSpace(in.School.Name)
		if in.School.Name == "" {
			in.School = nil
		} else {
			if !schoolTypes[in.School.Type] {
				bad("school.type is not valid")
			}
			if !curricula[in.School.Curriculum] {
				bad("school.curriculum is not valid")
			}
			if in.School.StartYear != 0 && !inRange(in.School.StartYear, 1990, today.Year()) {
				bad("school.startYear is not valid")
			}
		}
	}
	for i, b := range in.Baseline {
		if !domains[b.Domain] || !inRange(b.Level, 1, 4) || !inRange(b.Enjoyment, 0, 4) {
			bad("baseline[%d] is not valid", i)
		}
	}
	for i, it := range in.Interests {
		if Slug(it.Name) == "" || !inRange(it.Score, 1, 4) {
			bad("interests[%d] is not valid", i)
		}
	}
	for i, s := range in.Sports {
		if Slug(s.Name) == "" || !inRange(s.Level, 1, 4) || !inRange(s.Enjoyment, 0, 4) || s.HoursPerWeek < 0 || s.HoursPerWeek > 40 {
			bad("sports[%d] is not valid", i)
		}
	}
	for i, m := range in.Methods {
		if Slug(m) == "" {
			bad("methods[%d] is not valid", i)
		}
	}
	for i, c := range in.Competitions {
		if Slug(c.Name) == "" || !compTypes[c.Type] || tiers[c.Tier] == 0 || !inRange(c.Year, 1990, today.Year()) {
			bad("competitions[%d] is not valid", i)
		}
	}
	if in.Goals.WeeklyHourCap < 0 || in.Goals.WeeklyHourCap > 60 {
		bad("goals.weeklyHourCap must be 0 to 60")
	}
	if len(in.Baseline) > 8 || len(in.Interests) > 30 || len(in.Sports) > 30 || len(in.Methods) > 30 || len(in.Competitions) > 50 || len(in.Goals.Majors) > 10 {
		bad("too many items in one list")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// Statements turns a validated input into the graph writes for one child.
// newID must return a fresh unique id on every call.
func (in *OnboardingInput) Statements(userID, childID string, today time.Time, newID func() string) []Statement {
	var out []Statement
	add := func(command string, params map[string]any) { out = append(out, Cypher(command, params)) }
	day := today.Format("2006-01-02")

	add(`MATCH (u:User {id: $userId})
CREATE (c:Child {id: $childId, nickname: $nickname, birthDate: $birthDate, level: $level, grade: $grade,
  weeklyHourCap: $weeklyHourCap, aiConsent: $aiConsent, createdAt: $today})
CREATE (u)-[:GUARDIAN_OF]->(c)`,
		map[string]any{"userId": userID, "childId": childID, "nickname": in.Child.Nickname, "birthDate": in.Child.BirthDate,
			"level": in.Position.Level, "grade": in.Position.Grade, "weeklyHourCap": in.Goals.WeeklyHourCap,
			"aiConsent": in.Goals.AIConsent, "today": day})

	// Stages: earlier ones are done, the current one is active. History stays unknown, not zero.
	currentStage := StageForGrade(in.Position.Grade)
	if in.Position.Level == "university" {
		currentStage = "national_university"
	}
	for _, s := range nationalStages {
		if currentStage == "" {
			break
		}
		status := "done"
		if s.ID == currentStage {
			status = "active"
		}
		add(`MATCH (c:Child {id: $childId}), (s:Stage {id: $stageId})
MERGE (c)-[e:ENROLLED_IN]->(s) SET e.status = $status, e.source = 'onboarding'`,
			map[string]any{"childId": childID, "stageId": s.ID, "status": status})
		if s.ID == currentStage {
			break
		}
	}

	if in.School != nil {
		schoolID := "school_" + Slug(in.School.Name)
		add("MERGE (s:School {id: $id}) SET s.name = $name, s.type = $type",
			map[string]any{"id": schoolID, "name": in.School.Name, "type": in.School.Type})
		add(`MATCH (c:Child {id: $childId}), (s:School {id: $schoolId}), (k:Curriculum {id: $curriculumId})
MERGE (s)-[:USES]->(k)
MERGE (c)-[a:ATTENDS]->(s) SET a.status = 'active', a.startYear = $startYear`,
			map[string]any{"childId": childID, "schoolId": schoolID, "curriculumId": in.School.Curriculum, "startYear": in.School.StartYear})
	}

	// Baseline: a guardian estimate. It carries a low weight so later evidence overrides it.
	if school := StageForGrade(in.Position.Grade); school != "" {
		for _, b := range in.Baseline {
			competencyID := b.Domain + "_" + school
			params := map[string]any{"childId": childID, "competencyId": competencyID, "level": b.Level, "today": day, "observationId": newID()}
			set, props := "", ""
			if b.Enjoyment > 0 {
				params["enjoyment"] = b.Enjoyment
				set, props = ", m.enjoyment = $enjoyment", ", enjoyment: $enjoyment"
			}
			add(`MATCH (c:Child {id: $childId}), (k:Competency {id: $competencyId})
MERGE (c)-[m:MASTERS]->(k) SET m.level = $level, m.asOf = $today, m.source = 'onboarding'`+set+`
CREATE (o:Observation {id: $observationId, date: $today, source: 'onboarding', weight: 0.4, level: $level`+props+`})
CREATE (o)-[:ABOUT]->(c)
CREATE (o)-[:MEASURES]->(k)`, params)
		}
	}

	for _, it := range in.Interests {
		id := "interest_" + Slug(it.Name)
		add("MERGE (i:Interest {id: $id}) ON CREATE SET i.name = $name",
			map[string]any{"id": id, "name": strings.TrimSpace(it.Name)})
		add(`MATCH (c:Child {id: $childId}), (i:Interest {id: $id})
MERGE (c)-[r:INTERESTED_IN]->(i) SET r.score = $score, r.asOf = $today, r.source = 'onboarding'`,
			map[string]any{"childId": childID, "id": id, "score": it.Score, "today": day})
	}

	for _, s := range in.Sports {
		id := Slug(s.Name) // seeded sports use the bare slug: badminton, swimming, ...
		add("MERGE (s:Sport {id: $id}) ON CREATE SET s.name = $name, s.custom = true",
			map[string]any{"id": id, "name": strings.TrimSpace(s.Name)})
		params := map[string]any{"childId": childID, "id": id, "level": s.Level, "hoursPerWeek": s.HoursPerWeek, "today": day}
		set := ""
		if s.Enjoyment > 0 {
			params["enjoyment"] = s.Enjoyment
			set = ", r.enjoyment = $enjoyment"
		}
		add(`MATCH (c:Child {id: $childId}), (s:Sport {id: $id})
MERGE (c)-[r:PRACTICES]->(s) SET r.level = $level, r.hoursPerWeek = $hoursPerWeek, r.status = 'active', r.asOf = $today, r.source = 'onboarding'`+set, params)
	}

	for _, m := range in.Methods {
		id := Slug(m)
		add("MERGE (m:Method {id: $id}) ON CREATE SET m.name = $name, m.custom = true",
			map[string]any{"id": id, "name": strings.TrimSpace(m)})
		add("MATCH (c:Child {id: $childId}), (m:Method {id: $id}) MERGE (c)-[:LEARNS_WITH]->(m)",
			map[string]any{"childId": childID, "id": id})
	}

	for _, c := range in.Competitions {
		competitionID := "competition_" + Slug(c.Name)
		editionID := fmt.Sprintf("%s_%d", competitionID, c.Year)
		roundID := editionID + "_" + c.Tier
		add("MERGE (k:Competition {id: $id}) ON CREATE SET k.name = $name, k.type = $type",
			map[string]any{"id": competitionID, "name": strings.TrimSpace(c.Name), "type": c.Type})
		add(`MATCH (k:Competition {id: $competitionId})
MERGE (e:CompetitionEdition {id: $editionId}) SET e.year = $year
MERGE (k)-[:HAS_EDITION]->(e)
MERGE (r:Round {id: $roundId}) SET r.tier = $tier, r.sequence = $sequence
MERGE (e)-[:HAS_ROUND]->(r)`,
			map[string]any{"competitionId": competitionID, "editionId": editionID, "year": c.Year,
				"roundId": roundID, "tier": c.Tier, "sequence": tiers[c.Tier]})
		add(`MATCH (c:Child {id: $childId}), (r:Round {id: $roundId})
MERGE (c)-[p:PARTICIPATES_IN]->(r) SET p.result = $result, p.source = 'onboarding'`,
			map[string]any{"childId": childID, "roundId": roundID, "result": strings.TrimSpace(c.Result)})
	}

	for _, name := range in.Goals.Majors {
		if Slug(name) == "" {
			continue
		}
		id := "major_" + Slug(name)
		add("MERGE (m:Major {id: $id}) ON CREATE SET m.name = $name",
			map[string]any{"id": id, "name": strings.TrimSpace(name)})
		add("MATCH (c:Child {id: $childId}), (m:Major {id: $id}) MERGE (c)-[:AIMS_FOR]->(m)",
			map[string]any{"childId": childID, "id": id})
	}
	return out
}
