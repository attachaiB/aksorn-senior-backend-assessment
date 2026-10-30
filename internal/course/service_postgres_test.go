package course_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aksornproject/aksorn-senior-backend-assessment/internal/course"
	"github.com/aksornproject/aksorn-senior-backend-assessment/internal/infra"
)

func TestServiceWithPostgres(t *testing.T) {
	ctx := context.Background()

	connections, err := infra.Open(ctx)
	if err != nil {
		t.Skipf("exercise database not available (run make setup): %v", err)
	}

	t.Cleanup(connections.Close)

	prefix := fmt.Sprintf("pgtest-%d", time.Now().UnixNano())

	t.Cleanup(func() {
		_, _ = connections.Postgres.Exec(
			context.Background(),
			`DELETE FROM attempts WHERE learner_id LIKE $1`,
			prefix+"%",
		)
	})

	service := course.NewService(course.NewRepository(connections.Postgres))
	learnerA, learnerB := prefix+"-a", prefix+"-b"

	recordScores(t, service, learnerA,
		score{"introduction", 60},
		score{"introduction", 20},
		score{"practice", 0},
		score{"challenge", 85},
	)
	recordScores(t, service, learnerB, score{"practice", 100})

	a := progressOf(t, service, learnerA)
	assertLesson(t, a, "introduction", intPtr(60), true)
	assertLesson(t, a, "practice", intPtr(0), false)
	assertLesson(t, a, "challenge", intPtr(85), true)

	if a.ProgressPercent != 50 || a.Completed {
		t.Errorf("learner A = %d%%, completed %v; want 50%%, false",
			a.ProgressPercent, a.Completed)
	}

	b := progressOf(t, service, learnerB)
	assertLesson(t, b, "introduction", nil, false)
	assertLesson(t, b, "practice", intPtr(100), true)

	if b.ProgressPercent != 50 {
		t.Errorf("learner B = %d%%, want 50%%", b.ProgressPercent)
	}

	attempt := course.Attempt{
		AttemptID: learnerA + "-final",
		LearnerID: learnerA,
		CourseID:  course.CourseID,
		LessonID:  "practice",
		Score:     70,
	}
	if err := service.RecordAttempt(ctx, attempt); err != nil {
		t.Fatalf("RecordAttempt error = %v", err)
	}

	a = progressOf(t, service, learnerA)
	if a.ProgressPercent != 100 || !a.Completed {
		t.Errorf("learner A = %d%%, completed %v; want 100%%, true",
			a.ProgressPercent, a.Completed)
	}
}
