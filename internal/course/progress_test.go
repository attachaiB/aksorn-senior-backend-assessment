package course_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aksornproject/aksorn-senior-backend-assessment/internal/course"
)

type memoryStore struct {
	attempts []course.Attempt
}

func (m *memoryStore) Add(_ context.Context, attempt course.Attempt) error {
	m.attempts = append(m.attempts, attempt)

	return nil
}

func (m *memoryStore) List(
	_ context.Context,
	learnerID, courseID string,
) ([]course.Attempt, error) {
	found := []course.Attempt{}

	for _, attempt := range m.attempts {
		if attempt.LearnerID == learnerID && attempt.CourseID == courseID {
			found = append(found, attempt)
		}
	}

	return found, nil
}

type failingStore struct{ err error }

func (f failingStore) Add(context.Context, course.Attempt) error { return f.err }

func (f failingStore) List(context.Context, string, string) ([]course.Attempt, error) {
	return nil, f.err
}

type score struct {
	lesson string
	value  int
}

func recordScores(t *testing.T, service *course.Service, learnerID string, scores ...score) {
	t.Helper()

	for i, s := range scores {
		attempt := course.Attempt{
			AttemptID: fmt.Sprintf("%s-attempt-%d", learnerID, i),
			LearnerID: learnerID,
			CourseID:  course.CourseID,
			LessonID:  s.lesson,
			Score:     s.value,
		}
		if err := service.RecordAttempt(context.Background(), attempt); err != nil {
			t.Fatalf("RecordAttempt(%+v) error = %v", attempt, err)
		}
	}
}

func progressOf(t *testing.T, service *course.Service, learnerID string) course.Progress {
	t.Helper()

	progress, err := service.Progress(context.Background(), learnerID, course.CourseID)
	if err != nil {
		t.Fatalf("Progress(%q) error = %v", learnerID, err)
	}

	return progress
}

func lessonOf(t *testing.T, progress course.Progress, lessonID string) course.LessonProgress {
	t.Helper()

	for _, lesson := range progress.Lessons {
		if lesson.LessonID == lessonID {
			return lesson
		}
	}

	t.Fatalf("lesson %q missing from progress %+v", lessonID, progress)

	return course.LessonProgress{}
}

func assertLesson(
	t *testing.T,
	progress course.Progress,
	lessonID string,
	wantBest *int,
	wantPassed bool,
) {
	t.Helper()

	got := lessonOf(t, progress, lessonID)

	switch {
	case wantBest == nil && got.BestScore != nil:
		t.Errorf("%s best_score = %d, want null", lessonID, *got.BestScore)
	case wantBest != nil && got.BestScore == nil:
		t.Errorf("%s best_score = null, want %d", lessonID, *wantBest)
	case wantBest != nil && *got.BestScore != *wantBest:
		t.Errorf("%s best_score = %d, want %d", lessonID, *got.BestScore, *wantBest)
	}

	if got.Passed != wantPassed {
		t.Errorf("%s passed = %v, want %v", lessonID, got.Passed, wantPassed)
	}
}

func intPtr(v int) *int { return &v }

func TestProgressWithoutAttemptsReturnsAllLessonsEmpty(t *testing.T) {
	service := course.NewService(&memoryStore{})

	progress := progressOf(t, service, "learner-new")

	if len(progress.Lessons) != 3 {
		t.Fatalf("got %d lessons, want 3", len(progress.Lessons))
	}

	for _, lessonID := range []string{"introduction", "practice", "challenge"} {
		assertLesson(t, progress, lessonID, nil, false)
	}

	if progress.ProgressPercent != 0 || progress.Completed {
		t.Errorf("progress = %d%%, completed = %v; want 0%%, false",
			progress.ProgressPercent, progress.Completed)
	}

	if progress.LearnerID != "learner-new" || progress.CourseID != course.CourseID {
		t.Errorf("ids = %q/%q", progress.LearnerID, progress.CourseID)
	}
}

func TestProgressPassingThresholds(t *testing.T) {
	tests := []struct {
		lesson     string
		score      int
		wantPassed bool
	}{
		{"introduction", 59, false},
		{"introduction", 60, true},
		{"practice", 69, false},
		{"practice", 70, true},
		{"challenge", 79, false},
		{"challenge", 80, true},
		{"introduction", 100, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%d", tt.lesson, tt.score), func(t *testing.T) {
			service := course.NewService(&memoryStore{})
			recordScores(t, service, "learner-001", score{tt.lesson, tt.score})

			progress := progressOf(t, service, "learner-001")
			assertLesson(t, progress, tt.lesson, intPtr(tt.score), tt.wantPassed)
		})
	}
}

func TestProgressZeroScoreIsReportedAsZeroNotNull(t *testing.T) {
	service := course.NewService(&memoryStore{})
	recordScores(t, service, "learner-001", score{"practice", 0})

	progress := progressOf(t, service, "learner-001")
	assertLesson(t, progress, "practice", intPtr(0), false)
	assertLesson(t, progress, "introduction", nil, false)

	body, err := json.Marshal(lessonOf(t, progress, "practice"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), `"best_score":0`) {
		t.Errorf("practice JSON = %s, want best_score 0", body)
	}

	body, err = json.Marshal(lessonOf(t, progress, "introduction"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), `"best_score":null`) {
		t.Errorf("introduction JSON = %s, want best_score null", body)
	}
}

func TestProgressUsesBestOfMultipleAttempts(t *testing.T) {
	service := course.NewService(&memoryStore{})
	recordScores(t, service, "learner-001",
		score{"practice", 65},
		score{"practice", 90},
		score{"practice", 72},
	)

	progress := progressOf(t, service, "learner-001")
	assertLesson(t, progress, "practice", intPtr(90), true)
}

func TestProgressLaterLowerScoreKeepsBestAndPass(t *testing.T) {
	service := course.NewService(&memoryStore{})
	recordScores(t, service, "learner-001",
		score{"introduction", 60},
		score{"introduction", 20},
	)

	progress := progressOf(t, service, "learner-001")
	assertLesson(t, progress, "introduction", intPtr(60), true)

	if progress.ProgressPercent != 50 {
		t.Errorf("progress = %d%%, want 50%%", progress.ProgressPercent)
	}
}

func TestProgressLaterHigherScoreTurnsFailIntoPass(t *testing.T) {
	service := course.NewService(&memoryStore{})
	recordScores(t, service, "learner-001",
		score{"practice", 50},
		score{"practice", 75},
	)

	progress := progressOf(t, service, "learner-001")
	assertLesson(t, progress, "practice", intPtr(75), true)
}

func TestProgressCountsOnlyRequiredLessons(t *testing.T) {
	tests := []struct {
		name          string
		scores        []score
		wantPercent   int
		wantCompleted bool
	}{
		{"nothing passed", []score{{"introduction", 59}, {"practice", 69}}, 0, false},
		{"only optional passed", []score{{"challenge", 100}}, 0, false},
		{"introduction passed", []score{{"introduction", 60}}, 50, false},
		{"practice passed", []score{{"practice", 70}}, 50, false},
		{
			"introduction and optional passed",
			[]score{{"introduction", 60}, {"challenge", 80}},
			50,
			false,
		},
		{
			"introduction passed, practice just below",
			[]score{{"introduction", 60}, {"practice", 69}},
			50,
			false,
		},
		{"both required passed", []score{{"introduction", 60}, {"practice", 70}}, 100, true},
		{
			"both required passed, optional failed",
			[]score{{"introduction", 100}, {"practice", 100}, {"challenge", 10}},
			100,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := course.NewService(&memoryStore{})
			recordScores(t, service, "learner-001", tt.scores...)

			progress := progressOf(t, service, "learner-001")
			if progress.ProgressPercent != tt.wantPercent {
				t.Errorf("progress = %d%%, want %d%%", progress.ProgressPercent, tt.wantPercent)
			}

			if progress.Completed != tt.wantCompleted {
				t.Errorf("completed = %v, want %v", progress.Completed, tt.wantCompleted)
			}
		})
	}
}

func TestProgressOptionalLessonHasOwnResult(t *testing.T) {
	service := course.NewService(&memoryStore{})
	recordScores(t, service, "learner-001", score{"challenge", 85}, score{"challenge", 40})

	progress := progressOf(t, service, "learner-001")
	assertLesson(t, progress, "challenge", intPtr(85), true)
}

func TestProgressIsolatesLearners(t *testing.T) {
	service := course.NewService(&memoryStore{})
	recordScores(t, service, "learner-a",
		score{"introduction", 100},
		score{"practice", 100},
	)
	recordScores(t, service, "learner-b", score{"introduction", 10})

	a := progressOf(t, service, "learner-a")
	b := progressOf(t, service, "learner-b")
	upper := progressOf(t, service, "LEARNER-A")

	if !a.Completed || a.ProgressPercent != 100 {
		t.Errorf("learner-a = %d%%, completed %v; want 100%%, true",
			a.ProgressPercent, a.Completed)
	}

	assertLesson(t, b, "introduction", intPtr(10), false)
	assertLesson(t, b, "practice", nil, false)

	if b.ProgressPercent != 0 {
		t.Errorf("learner-b progress = %d%%, want 0%%", b.ProgressPercent)
	}

	assertLesson(t, upper, "introduction", nil, false)
}

func TestRecordAttemptStoresEveryAcceptedAttempt(t *testing.T) {
	store := &memoryStore{}
	service := course.NewService(store)
	recordScores(t, service, "learner-001",
		score{"introduction", 40},
		score{"introduction", 40},
		score{"introduction", 80},
	)

	if len(store.attempts) != 3 {
		t.Fatalf("stored %d attempts, want 3", len(store.attempts))
	}
}

func TestRecordAttemptDoesNotStoreInvalidInput(t *testing.T) {
	store := &memoryStore{}
	service := course.NewService(store)

	valid := course.Attempt{
		AttemptID: "a1", LearnerID: "l1", CourseID: course.CourseID,
		LessonID: "practice", Score: 50,
	}
	tooHigh, negative, blankLearner, otherCourse := valid, valid, valid, valid
	tooHigh.Score = 101
	negative.Score = -1
	blankLearner.LearnerID = " "
	otherCourse.CourseID = "course-999"

	for _, attempt := range []course.Attempt{tooHigh, negative, blankLearner, otherCourse} {
		if err := service.RecordAttempt(context.Background(), attempt); err == nil {
			t.Errorf("RecordAttempt(%+v) succeeded, want error", attempt)
		}
	}

	if len(store.attempts) != 0 {
		t.Errorf("stored %d invalid attempts, want 0", len(store.attempts))
	}
}

func TestStorageErrorsArePropagated(t *testing.T) {
	storageErr := errors.New("database unavailable")
	service := course.NewService(failingStore{err: storageErr})

	attempt := course.Attempt{
		AttemptID: "a1", LearnerID: "l1", CourseID: course.CourseID,
		LessonID: "introduction", Score: 60,
	}
	if err := service.RecordAttempt(context.Background(), attempt); !errors.Is(err, storageErr) {
		t.Errorf("RecordAttempt error = %v, want storage error", err)
	}

	_, err := service.Progress(context.Background(), "l1", course.CourseID)
	if !errors.Is(err, storageErr) {
		t.Errorf("Progress error = %v, want storage error", err)
	}
}
