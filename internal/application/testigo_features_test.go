package application

import (
	"context"
	"errors"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

type repositoryUser struct {
	repository applicationtest.RepositoryDirectory
}

func (u repositoryUser) Create(ctx context.Context, repository domain.Repository) (domain.Repository, error) {
	return u.repository.Upsert(ctx, repository)
}

func TestGeneratedTestigoDoubles(t *testing.T) {
	testigo.Run(t, "records a generated spy delegating to a generated stub", func(t *testing.T) {
		input := applicationtest.Repository.WithFullName("owner/repo").Bare()
		stub := applicationtest.NewRepositoryDirectoryStub(applicationtest.Repository.WithID(42).WithFullName(input.FullName))
		spy := applicationtest.NewRepositoryDirectorySpy(t, stub)
		user := repositoryUser{repository: spy}

		result, err := user.Create(context.Background(), input)

		assert.NoError(t, err)
		assert.Equal(t, result.ID, int64(42))
		assert.That(user).Called(spy.Upsert).WithParams(context.Background(), input).Once()
	})

	testigo.Run(t, "returns the configured error from a generated error stub", func(t *testing.T) {
		want := errors.New("repository unavailable")
		stub := applicationtest.NewRepositoryDirectoryErrorsErrorStub(applicationtest.Repository, want)

		_, err := stub.Upsert(context.Background(), applicationtest.Repository.Bare())

		assert.ErrorIs(t, err, want)
	})
}

func TestGeneratedMemDBAndRandomFactory(t *testing.T) {
	testigo.Run(t, "supports keyed updates and filtered queries", func(t *testing.T) {
		first := applicationtest.ActiveRule.WithRuleID("AIR-GO-001").WithLanguage("go").WithStatus(domain.RuleActive).Bare()
		second := applicationtest.ActiveRule.WithRuleID("AIR-PHP-001").WithLanguage("php").WithStatus(domain.RuleDisabled).Bare()
		db := applicationtest.ActiveRule.DB(first, second)

		updated, found := db.Update("AIR-GO-001", func(rule domain.Rule) domain.Rule {
			rule.Title = "updated"
			return rule
		})
		active := db.Where(func(rule domain.Rule) bool { return rule.Status == domain.RuleActive })

		assert.True(t, found, "the generated memdb should find the keyed rule")
		assert.Equal(t, updated.Title, "updated")
		assert.Equal(t, len(active), 1)
	})
}

func TestGeneratedCollectionStub(t *testing.T) {
	testigo.Run(t, "returns one fresh fixture for repository-shaped queries", func(t *testing.T) {
		stub := NewSnapshotRulesStub(ActiveRule.WithRuleID("AIR-GO-001"))

		rules, err := stub.ListByStatus(context.Background(), domain.RuleActive)

		assert.NoError(t, err)
		assert.Len(t, rules, 1)
		assert.Equal(t, rules[0].RuleID, "AIR-GO-001")
	})
}
