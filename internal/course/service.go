package course

import (
	"context"
	"fmt"
	"strings"
)

type AttemptStore interface {
	Add(ctx context.Context, attempt Attempt) error
	List(ctx context.Context, learnerID, courseID string) ([]Attempt, error)
}

type Service struct {
	repo AttemptStore
}

func NewService(repo AttemptStore) *Service {
	return &Service{repo: repo}
}

func (s *Service) RecordAttempt(ctx context.Context, attempt Attempt) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := ValidateAttempt(attempt); err != nil {
		return err
	}

	if err := s.repo.Add(ctx, attempt); err != nil {
		return fmt.Errorf("record attempt: %w", err)
	}

	return nil
}

func (s *Service) Progress(ctx context.Context, learnerID, courseID string) (Progress, error) {
	if err := ctx.Err(); err != nil {
		return Progress{}, err
	}

	if strings.TrimSpace(learnerID) == "" {
		return Progress{}, ErrInvalidInput
	}

	if courseID != CourseID {
		return Progress{}, ErrUnknownCourse
	}

	attempts, err := s.repo.List(ctx, learnerID, courseID)
	if err != nil {
		return Progress{}, fmt.Errorf("load attempts: %w", err)
	}

	course := FixedCourse()
	best := make(map[string]int, len(course.Lessons))

	for _, attempt := range attempts {
		if current, seen := best[attempt.LessonID]; !seen || attempt.Score > current {
			best[attempt.LessonID] = attempt.Score
		}
	}

	progress := Progress{
		LearnerID: learnerID,
		CourseID:  course.ID,
		Lessons:   make([]LessonProgress, 0, len(course.Lessons)),
	}

	requiredLessons, requiredPassed := 0, 0

	for _, lesson := range course.Lessons {
		result := LessonProgress{LessonID: lesson.ID}

		if score, ok := best[lesson.ID]; ok {
			result.BestScore = &score
			result.Passed = score >= lesson.PassingScore
		}

		if lesson.Required {
			requiredLessons++

			if result.Passed {
				requiredPassed++
			}
		}

		progress.Lessons = append(progress.Lessons, result)
	}

	if requiredLessons > 0 {
		progress.ProgressPercent = requiredPassed * 100 / requiredLessons
		progress.Completed = requiredPassed == requiredLessons
	}

	return progress, nil
}
