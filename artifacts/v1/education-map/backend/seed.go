package main

import (
	"context"
	"log"
)

const seedVersion = "seed-001-catalog"

type stageSeed struct {
	ID, Name, Level string
	Sequence        int
	GradeFrom       int
	GradeTo         int
}

// nationalStages is the time axis of the graph (Kurikulum Merdeka phases).
var nationalStages = []stageSeed{
	{"national_a", "Fase A", "primary", 1, 1, 2},
	{"national_b", "Fase B", "primary", 2, 3, 4},
	{"national_c", "Fase C", "primary", 3, 5, 6},
	{"national_d", "Fase D", "junior", 4, 7, 9},
	{"national_e", "Fase E", "senior", 5, 10, 10},
	{"national_f", "Fase F", "senior", 6, 11, 12},
	{"national_university", "Kuliah S1", "university", 7, 13, 16},
}

var cambridgeStages = []stageSeed{
	{"cambridge_primary", "Cambridge Primary", "primary", 1, 0, 0},
	{"cambridge_lower_secondary", "Cambridge Lower Secondary", "junior", 2, 0, 0},
	{"cambridge_upper_secondary", "Cambridge Upper Secondary (IGCSE)", "senior", 3, 0, 0},
	{"cambridge_advanced", "Cambridge Advanced (AS & A Level)", "senior", 4, 0, 0},
}

// competencyDomains maps a domain to its display name and talent area.
var competencyDomains = []struct{ ID, Name, Talent string }{
	{"numeracy", "Numerasi", "logic"},
	{"literacy", "Literasi", "language"},
	{"science", "Sains", "science"},
	{"computing", "Komputasi", "logic"},
}

var talentSeeds = []struct{ ID, Name string }{
	{"logic", "Logika dan komputasi"},
	{"language", "Bahasa"},
	{"science", "Sains"},
	{"visual", "Visual dan seni"},
	{"kinesthetic", "Kinestetik"},
}

var sportSeeds = []struct {
	ID, Name    string
	Competitive bool
}{
	{"badminton", "Badminton", true},
	{"swimming", "Renang", true},
	{"basketball", "Basket", true},
	{"aikido", "Aikido", false},
}

// StageForGrade returns the national stage id for a school grade (1-12).
func StageForGrade(grade int) string {
	for _, s := range nationalStages {
		if grade >= s.GradeFrom && grade <= s.GradeTo {
			return s.ID
		}
	}
	return ""
}

// seedStatements builds the starter catalog. Every statement is an idempotent MERGE.
func seedStatements() []Statement {
	var out []Statement
	add := func(command string, params map[string]any) { out = append(out, Cypher(command, params)) }

	add("MERGE (c:Curriculum {id: $id}) SET c.name = $name", map[string]any{"id": "national", "name": "Kurikulum Nasional"})
	add("MERGE (c:Curriculum {id: $id}) SET c.name = $name", map[string]any{"id": "cambridge", "name": "Cambridge"})
	add("MERGE (c:Curriculum {id: $id}) SET c.name = $name", map[string]any{"id": "ib", "name": "International Baccalaureate"})
	add("MERGE (c:Curriculum {id: $id}) SET c.name = $name", map[string]any{"id": "other", "name": "Lainnya"})

	stages := func(curriculum string, list []stageSeed) {
		for i, s := range list {
			add("MERGE (s:Stage {id: $id}) SET s.name = $name, s.level = $level, s.sequence = $sequence, s.gradeFrom = $gradeFrom, s.gradeTo = $gradeTo",
				map[string]any{"id": s.ID, "name": s.Name, "level": s.Level, "sequence": s.Sequence, "gradeFrom": s.GradeFrom, "gradeTo": s.GradeTo})
			add("MATCH (s:Stage {id: $stage}), (c:Curriculum {id: $curriculum}) MERGE (s)-[:PART_OF]->(c)",
				map[string]any{"stage": s.ID, "curriculum": curriculum})
			if i > 0 {
				add("MATCH (a:Stage {id: $from}), (b:Stage {id: $to}) MERGE (a)-[:NEXT]->(b)",
					map[string]any{"from": list[i-1].ID, "to": s.ID})
			}
		}
	}
	stages("national", nationalStages)
	stages("cambridge", cambridgeStages)

	for _, t := range talentSeeds {
		add("MERGE (t:Talent {id: $id}) SET t.name = $name", map[string]any{"id": t.ID, "name": t.Name})
	}

	// One generic competency per domain and school stage, chained by PREREQUISITE_OF.
	// Replace or refine these with your own competency catalog later.
	school := nationalStages[:6]
	for _, d := range competencyDomains {
		for i, s := range school {
			id := d.ID + "_" + s.ID
			add("MERGE (k:Competency {id: $id}) SET k.name = $name, k.domain = $domain",
				map[string]any{"id": id, "name": d.Name + " " + s.Name, "domain": d.ID})
			add("MATCH (k:Competency {id: $id}), (s:Stage {id: $stage}) MERGE (k)-[:TAUGHT_IN]->(s)",
				map[string]any{"id": id, "stage": s.ID})
			add("MATCH (k:Competency {id: $id}), (t:Talent {id: $talent}) MERGE (k)-[:INDICATES]->(t)",
				map[string]any{"id": id, "talent": d.Talent})
			if i > 0 {
				add("MATCH (a:Competency {id: $from}), (b:Competency {id: $to}) MERGE (a)-[:PREREQUISITE_OF]->(b)",
					map[string]any{"from": d.ID + "_" + school[i-1].ID, "to": id})
			}
		}
	}

	for _, s := range sportSeeds {
		add("MERGE (s:Sport {id: $id}) SET s.name = $name, s.competitive = $competitive",
			map[string]any{"id": s.ID, "name": s.Name, "competitive": s.Competitive})
		add("MATCH (s:Sport {id: $id}), (t:Talent {id: 'kinesthetic'}) MERGE (s)-[:INDICATES]->(t)",
			map[string]any{"id": s.ID})
	}
	add("MERGE (m:Method {id: $id}) SET m.name = $name, m.subject = $subject",
		map[string]any{"id": "abacus", "name": "Sempoa", "subject": "numeracy"})
	return out
}

// Seed loads the starter catalog once.
func Seed(ctx context.Context, db *DB) error {
	done, err := migrationApplied(ctx, db, seedVersion)
	if err != nil || done {
		return err
	}
	for _, st := range seedStatements() {
		if _, err := db.Run(ctx, st, ""); err != nil {
			return err
		}
	}
	if err := markMigration(ctx, db, seedVersion); err != nil {
		return err
	}
	log.Printf("catalog seeded: %s", seedVersion)
	return nil
}
