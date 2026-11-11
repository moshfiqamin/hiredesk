package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// applicationColumns is the column list every query here returns, in the
// order scanApplication reads them. Qualified with the alias a, which every
// query gives the applications table, so the same list works in a join.
const applicationColumns = `a.id::text, a.job_post_id::text, a.candidate_id::text, a.status, a.cv_url, a.submitted_at, a.updated_at`

// constraintOneApplicationPerPost is the unique constraint, from migration
// 000004, that allows one application per candidate per job post. Matched by
// name so that a unique violation from any other index is not mistaken for
// a repeat application.
const constraintOneApplicationPerPost = "applications_job_post_id_candidate_id_key"

// applicationOrder puts the newest first. id breaks ties between rows
// submitted in the same microsecond, so the order is stable across calls.
// Both columns run in the same direction, which is what lets a row
// comparison on (submitted_at, id) express "after the cursor".
const applicationOrder = `ORDER BY a.submitted_at DESC, a.id DESC`

// ApplicationStore reads the applications table.
type ApplicationStore struct {
	db *pgxpool.Pool
}

// NewApplicationStore returns an ApplicationStore that runs its queries on db.
func NewApplicationStore(db *pgxpool.Pool) *ApplicationStore {
	return &ApplicationStore{db: db}
}

// ListApplications returns one page of the applications viewer may see,
// newest first, narrowed by filter, together with how many applications
// match the filter in all, and whether a further page follows.
//
// Who sees what is decided in the SQL, so rows the viewer may not see never
// leave the database:
//
//   - an admin, every application;
//   - a candidate, the ones they submitted;
//   - an interviewer, the ones they are assigned to.
//
// Any other role gets an error matching domain.ErrForbidden.
//
// The scope here and auth.CanViewApplication state the same rule twice, once
// for many rows and once for one. TestAuthorizationMatrix checks that they
// agree.
func (s *ApplicationStore) ListApplications(ctx context.Context, viewer domain.User, filter domain.ApplicationFilter) (apps []domain.Application, total int, more bool, err error) {
	const op = "list applications"

	// The WHERE clause is assembled from fixed fragments written in this
	// function. Every value — the viewer's ID, the filters, the cursor —
	// travels as a parameter, so nothing a client sends is ever part of the
	// SQL text.
	var where conditions

	switch viewer.Role {
	case domain.RoleAdmin:
		// No scope: an admin sees every row.

	case domain.RoleCandidate:
		// Served by applications_candidate_id_idx.
		where.add("a.candidate_id = " + where.arg(viewer.ID) + "::text::uuid")

	case domain.RoleInterviewer:
		// EXISTS rather than a JOIN keeps the FROM clause the same for every
		// role. It is answered from the unique index on (application_id,
		// interviewer_id), and cannot repeat a row however many assignment
		// rows match.
		where.add(`EXISTS (
			SELECT 1 FROM interviewer_assignments ia
			WHERE ia.application_id = a.id
			  AND ia.interviewer_id = ` + where.arg(viewer.ID) + `::text::uuid)`)

	default:
		// Deny by default, as the policy does: a role nobody wrote a scope
		// for sees nothing, rather than falling through to the admin's view.
		return nil, 0, false, fmt.Errorf("%s: role %q: %w", op, viewer.Role, domain.ErrForbidden)
	}

	// Only the filters actually given become conditions, so the planner sees
	// "job_post_id = $2" and can use applications_job_post_id_status_idx,
	// instead of a "$2 IS NULL OR job_post_id = $2" it must plan for both.
	if filter.Status != "" {
		where.add("a.status = " + where.arg(filter.Status))
	}
	if filter.JobPostID != "" {
		where.add("a.job_post_id = " + where.arg(filter.JobPostID) + "::text::uuid")
	}

	// The count ignores the cursor: it is every match, not what is left.
	countQuery := `SELECT count(*) FROM applications a` + where.sql()
	countArgs := slices.Clone(where.args)

	// The page adds the cursor and the limit to a copy, so the count's
	// conditions and arguments are not disturbed.
	page := where.clone()
	if filter.After != nil {
		// A row comparison: true for every row after the cursor in a
		// (submitted_at DESC, id DESC) order, and matched by
		// applications_submitted_at_id_idx, so the scan starts at the cursor.
		page.add("(a.submitted_at, a.id) < (" + page.arg(filter.After.CreatedAt) + "::timestamptz, " +
			page.arg(filter.After.ID) + "::text::uuid)")
	}
	// One more than the page, to learn whether another page follows without
	// counting what remains.
	pageQuery := `SELECT ` + applicationColumns + ` FROM applications a` + page.sql() +
		` ` + applicationOrder + ` LIMIT ` + page.arg(filter.Limit+1)

	// Both statements in one read-only transaction at REPEATABLE READ, so
	// they read one snapshot: the total cannot disagree with the page because
	// an application arrived between the two.
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, false, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Read-only: there is nothing to undo, and Commit below ends it.

	if err = tx.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, false, translate(op+": count", err)
	}

	rows, err := tx.Query(ctx, pageQuery, page.args...)
	if err != nil {
		return nil, 0, false, translate(op, err)
	}

	// CollectRows reads every row through scanApplication, closes rows, and
	// returns rows.Err(), which is where an error part-way through the result
	// set is reported. A hand-written rows.Next loop must remember all three.
	apps, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Application, error) {
		app, scanErr := scanApplication(row)
		if scanErr != nil {
			return domain.Application{}, scanErr
		}
		return *app, nil
	})
	if err != nil {
		return nil, 0, false, translate(op, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, 0, false, translate(op+": commit", err)
	}

	if len(apps) > filter.Limit {
		apps, more = apps[:filter.Limit], true
	}
	// CollectRows returns a nil slice for no rows. An empty slice encodes as
	// [] rather than null, so a client gets one shape either way.
	if apps == nil {
		apps = []domain.Application{}
	}
	return apps, total, more, nil
}

// conditions builds a WHERE clause from fixed SQL fragments and a matching
// list of parameter values.
//
// arg is the only way a value enters a query: it appends the value to args
// and returns its placeholder, "$1", "$2" and so on. The placeholder is the
// only thing concatenated into the SQL, and it is produced here, never taken
// from input.
type conditions struct {
	parts []string
	args  []any
}

// arg records v as the next parameter and returns its placeholder.
func (c *conditions) arg(v any) string {
	c.args = append(c.args, v)
	return "$" + strconv.Itoa(len(c.args))
}

// add records one condition. The conditions are joined with AND.
func (c *conditions) add(sql string) {
	c.parts = append(c.parts, sql)
}

// sql returns " WHERE a AND b …", or "" when there are no conditions.
func (c *conditions) sql() string {
	if len(c.parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(c.parts, " AND ")
}

// clone returns a copy that can be extended without changing c.
func (c *conditions) clone() conditions {
	return conditions{parts: slices.Clone(c.parts), args: slices.Clone(c.args)}
}

// GetApplication returns the application with this ID, and whether viewerID
// is assigned to it as an interviewer. It returns an error matching
// domain.ErrNotFound if there is no such application, including when id is
// not a UUID.
//
// It does not decide whether the viewer may see the application; that is
// auth.CanViewApplication, which needs exactly the two facts returned here.
func (s *ApplicationStore) GetApplication(ctx context.Context, id, viewerID string) (*domain.Application, bool, error) {
	// Both facts in one round trip, so a refused request and a missing one do
	// the same work. With a second query only when the row exists, the time
	// taken would tell an outsider which IDs are real.
	//
	// EXISTS stops at the first matching row and is answered from the unique
	// index on (application_id, interviewer_id).
	const query = `
		SELECT ` + applicationColumns + `,
			EXISTS (
				SELECT 1
				FROM interviewer_assignments ia
				WHERE ia.application_id = a.id
				  AND ia.interviewer_id = $2::text::uuid
			)
		FROM applications a
		WHERE a.id = $1::text::uuid`

	var (
		app      domain.Application
		assigned bool
	)
	err := s.db.QueryRow(ctx, query, id, viewerID).Scan(
		&app.ID, &app.JobPostID, &app.CandidateID, &app.Status, &app.CVURL, &app.SubmittedAt, &app.UpdatedAt,
		&assigned,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == codeInvalidTextRepresentation {
			// No application can have an ID that is not a UUID.
			return nil, false, fmt.Errorf("get application: %w", domain.ErrNotFound)
		}
		return nil, false, translate("get application", err)
	}
	return &app, assigned, nil
}

// TransitionApplication moves the application with this ID to status to,
// records an application.status_changed event naming actorID and both
// statuses, and returns the application as stored.
//
// It fails with:
//
//   - an error matching domain.ErrNotFound if there is no such application;
//   - a *domain.TransitionError if the lifecycle does not allow the move;
//   - a *domain.NoVacanciesError if to is offer and every vacancy on the
//     post already has one.
//
// In none of those cases is anything written.
func (s *ApplicationStore) TransitionApplication(ctx context.Context, id, to, actorID string) (*domain.Application, error) {
	const op = "transition application"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	// FOR UPDATE serialises changes to one application. Two admins moving it
	// at once would otherwise both read the same status, both pass the check,
	// and record two events claiming the same "from".
	const lockApplication = `SELECT ` + applicationColumns + ` FROM applications a WHERE a.id = $1::text::uuid FOR UPDATE`

	app, err := scanApplication(tx.QueryRow(ctx, lockApplication, id))
	if err != nil {
		return nil, translateByID(op, err)
	}
	from := app.Status

	if err = domain.CheckApplicationTransition(from, to); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if to == domain.StatusOffer {
		if err = checkVacancy(ctx, tx, app.JobPostID); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	}

	const update = `
		UPDATE applications AS a
		SET status = $2, updated_at = now()
		WHERE a.id = $1::text::uuid
		RETURNING ` + applicationColumns

	app, err = scanApplication(tx.QueryRow(ctx, update, app.ID, to))
	if err != nil {
		return nil, translate(op, err)
	}

	// Same transaction as the UPDATE, so the trail can never show a change
	// that did not happen, nor miss one that did. Both statuses are stored:
	// "from" is what this transaction read under the lock, which no later
	// query could recover.
	const insertEvent = `
		INSERT INTO application_events (application_id, actor_id, type, payload)
		VALUES ($1::text::uuid, $2::text::uuid, $3,
		        jsonb_build_object('from', $4::text, 'to', $5::text))`

	if _, err = tx.Exec(ctx, insertEvent, app.ID, actorID, domain.EventApplicationStatusChanged, from, to); err != nil {
		return nil, translate(op+": record event", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, translate(op+": commit", err)
	}
	return app, nil
}

// checkVacancy returns a *domain.NoVacanciesError unless the job post has a
// vacancy without an offer.
//
// The post's row is locked first, so two offers on the same post are made
// one at a time: without the lock, two transactions could each count one
// remaining vacancy, and both fill it. FOR NO KEY UPDATE is the weakest
// lock that two offers cannot share. It still lets new rows reference the
// post, since a foreign-key insert takes only FOR KEY SHARE.
//
// The lock and the count must be two statements. At READ COMMITTED each
// statement reads from a snapshot taken when it starts. A single statement
// that waited for the lock would still count from its snapshot of before
// the wait, and miss the offer the other transaction has just committed.
// The second statement starts after the lock is held, so it sees that offer.
func checkVacancy(ctx context.Context, tx pgx.Tx, jobPostID string) error {
	const lockPost = `SELECT vacancies FROM job_posts WHERE id = $1::text::uuid FOR NO KEY UPDATE`

	var vacancies int
	if err := tx.QueryRow(ctx, lockPost, jobPostID).Scan(&vacancies); err != nil {
		return translate("lock job post", err)
	}

	// Served by applications_job_post_id_status_idx.
	const countOffers = `
		SELECT count(*)::int FROM applications
		WHERE job_post_id = $1::text::uuid AND status = 'offer'`

	var offers int
	if err := tx.QueryRow(ctx, countOffers, jobPostID).Scan(&offers); err != nil {
		return translate("count offers", err)
	}
	return domain.CheckVacancyForOffer(vacancies, offers)
}

// ApplicationHistory returns the answers of the application with this ID, in
// form order, and its events, oldest first. It does not check who may see
// them: call it only after auth.CanViewApplication has allowed the viewer.
func (s *ApplicationStore) ApplicationHistory(ctx context.Context, id string) ([]domain.Answer, []domain.ApplicationEvent, error) {
	const op = "application history"

	// Joined to questions only for the order: answers are shown in the order
	// the form asks them, not the order they were inserted.
	const answersQuery = `
		SELECT aa.id::text, aa.application_id::text, aa.question_id::text, aa.answer_text, aa.created_at
		FROM application_answers aa
		JOIN questions q ON q.id = aa.question_id
		WHERE aa.application_id = $1::text::uuid
		ORDER BY q.position`

	rows, err := s.db.Query(ctx, answersQuery, id)
	if err != nil {
		return nil, nil, translate(op, err)
	}
	answers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Answer, error) {
		var a domain.Answer
		scanErr := row.Scan(&a.ID, &a.ApplicationID, &a.QuestionID, &a.Text, &a.CreatedAt)
		return a, scanErr
	})
	if err != nil {
		return nil, nil, translate(op, err)
	}

	// Served by application_events_application_id_created_at_idx. id breaks
	// ties between events of one transaction, which share a created_at:
	// now() is the transaction's start time, not the statement's.
	const eventsQuery = `
		SELECT id::text, application_id::text, actor_id::text, type, payload, created_at
		FROM application_events
		WHERE application_id = $1::text::uuid
		ORDER BY created_at, id`

	rows, err = s.db.Query(ctx, eventsQuery, id)
	if err != nil {
		return nil, nil, translate(op, err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ApplicationEvent, error) {
		var e domain.ApplicationEvent
		// actor_id is nullable, so it scans into a *string; payload is jsonb,
		// which pgx decodes into a map with encoding/json.
		scanErr := row.Scan(&e.ID, &e.ApplicationID, &e.ActorID, &e.Type, &e.Payload, &e.CreatedAt)
		return e, scanErr
	})
	if err != nil {
		return nil, nil, translate(op, err)
	}

	if answers == nil {
		answers = []domain.Answer{}
	}
	if events == nil {
		events = []domain.ApplicationEvent{}
	}
	return answers, events, nil
}

// ScoreCard returns the stored score of the application with this ID: every
// score row of its latest rubric version, each with its criterion and its
// evidence, in rubric order. It returns an error matching domain.ErrNotScored
// if the application has no scores.
//
// It does not check that the application exists, nor who may see its score:
// call it only after GetApplication and auth.CanViewScore.
func (s *ApplicationStore) ScoreCard(ctx context.Context, id string) (*domain.ScoreCard, error) {
	const op = "score card"

	// One statement for the whole card, however many criteria the rubric
	// has. Each join is on a primary key, so each adds one index lookup per
	// score row, and scores_application_id_idx finds the rows themselves.
	//
	// Both evidence joins are LEFT: a score with no evidence answer must
	// still appear, and an inner join on questions would drop it again, since
	// q's join condition reads aa.
	//
	// The subquery keeps the card to one version. A rescore replaces the
	// rows, so there should only ever be one, but a card mixing two rubrics
	// would add up points from criteria that never coexisted.
	const query = `
		SELECT ` + criterionColumns + `,
			s.earned, s.available, s.detail, s.rubric_version,
			aa.id::text, q.prompt, aa.answer_text
		FROM scores s
		JOIN rubric_criteria rc ON rc.id = s.criterion_id
		LEFT JOIN application_answers aa ON aa.id = s.evidence_answer_id
		LEFT JOIN questions q ON q.id = aa.question_id
		WHERE s.application_id = $1::text::uuid
		  AND s.rubric_version = (
			SELECT max(latest.rubric_version) FROM scores latest
			WHERE latest.application_id = $1::text::uuid)
		ORDER BY rc.position`

	rows, err := s.db.Query(ctx, query, id)
	if err != nil {
		return nil, translate(op, err)
	}
	entries, err := pgx.CollectRows(rows, scanScoreEntry)
	if err != nil {
		return nil, translate(op, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s: %w", op, domain.ErrNotScored)
	}

	return &domain.ScoreCard{
		ApplicationID: id,
		// Every row has the same version: the WHERE clause says so.
		RubricVersion: entries[0].Criterion.RubricVersion,
		Entries:       entries,
	}, nil
}

// scanScoreEntry reads one row of the ScoreCard query.
func scanScoreEntry(row pgx.CollectableRow) (domain.ScoreEntry, error) {
	var (
		e            domain.ScoreEntry
		scoreVersion int
		// The three evidence columns are NULL together when the score has
		// no evidence answer, so they scan into pointers.
		answerID, prompt, answerText *string
	)
	c := &e.Criterion
	err := row.Scan(
		&c.ID, &c.Label, &c.Kind, &c.Weight, &c.Config, &c.QuestionID, &c.RubricVersion,
		&e.Earned, &e.Available, &e.Detail, &scoreVersion,
		&answerID, &prompt, &answerText,
	)
	if err != nil {
		return domain.ScoreEntry{}, err
	}
	// A score is written with its criterion's version, so the two agree;
	// the score's own copy is the one that counts if they ever do not.
	c.RubricVersion = scoreVersion

	if answerID != nil && prompt != nil && answerText != nil {
		e.Evidence = &domain.ScoreEvidence{AnswerID: *answerID, Question: *prompt, AnswerText: *answerText}
	}
	return e, nil
}

// SubmitApplication validates in against the job post's form and stores it:
// the application, every answer, one score per criterion of the post's
// current rubric, and an application.submitted event naming the candidate as
// the actor. Either all of it is written or none is.
//
// A post with no rubric gets an unscored application rather than a refusal;
// see scoreApplication.
//
// It fails with:
//
//   - an error matching domain.ErrNotFound if there is no such post, or it is
//     a draft, which a candidate may not see;
//   - a *domain.NotAcceptingError if the post is not open;
//   - a *domain.SubmissionError listing every problem with the input;
//   - an error matching domain.ErrAlreadyApplied (and so ErrDuplicate) if the
//     candidate has already applied to this post, including when the other
//     application is being submitted at the same moment;
//   - an error wrapping a *scoring.CriterionError if the rubric cannot be
//     applied, which only a rubric changed by hand can cause.
//
// On success it returns the stored application with its answers, in the
// order they were submitted.
func (s *ApplicationStore) SubmitApplication(ctx context.Context, in domain.NewApplication) (*domain.Application, error) {
	const op = "submit application"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin: %w", op, err)
	}
	// Undoes every statement below on an early return or a panic. After a
	// successful Commit it does nothing and returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	// FOR SHARE holds the post's row until this transaction ends, so
	// TransitionJobPost's FOR UPDATE waits rather than closing the post
	// between this check and the insert. Two submissions both take FOR SHARE,
	// which do not conflict, so candidates never queue behind one another.
	const lockPost = `SELECT status FROM job_posts WHERE id = $1::text::uuid FOR SHARE`

	var status string
	if err = tx.QueryRow(ctx, lockPost, in.JobPostID).Scan(&status); err != nil {
		return nil, translateByID(op, err)
	}
	if err = domain.CheckAcceptingApplications(status); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// The form cannot change under the lock: questions are editable only on a
	// draft, and this post is open.
	form, err := listQuestions(ctx, tx, in.JobPostID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	clean, err := domain.ValidateApplication(form, in)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// The alias lets applicationColumns, written for SELECTs with "a.", serve
	// in RETURNING as well.
	const insertApplication = `
		INSERT INTO applications AS a (job_post_id, candidate_id, cv_url)
		VALUES ($1::text::uuid, $2::text::uuid, $3)
		RETURNING ` + applicationColumns

	app, err := scanApplication(tx.QueryRow(ctx, insertApplication, clean.JobPostID, clean.CandidateID, clean.CVURL))
	if err != nil {
		// The only guard against a second application, and the only one that
		// holds when two arrive at once: the unique index refuses whichever
		// insert comes second, even if it waited for the first to commit.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation &&
			pgErr.ConstraintName == constraintOneApplicationPerPost {
			return nil, fmt.Errorf("%s: %w: %w", op, domain.ErrAlreadyApplied, err)
		}
		return nil, translate(op+": insert application", err)
	}

	app.Answers, err = insertAnswers(ctx, tx, app.ID, clean.Answers)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	app.RubricVersion, err = scoreApplication(ctx, tx, app)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Last, and inside the transaction: an event exists if and only if the
	// application it describes does. rubric_version is JSON null when the
	// application was not scored.
	const insertEvent = `
		INSERT INTO application_events (application_id, actor_id, type, payload)
		VALUES ($1::text::uuid, $2::text::uuid, $3,
		        jsonb_build_object('job_post_id', $4::text, 'answers', $5::int, 'rubric_version', $6::int))`

	// app's IDs, not the input's: the database's canonical lowercase form.
	if _, err = tx.Exec(ctx, insertEvent, app.ID, app.CandidateID, domain.EventApplicationSubmitted,
		app.JobPostID, len(app.Answers), app.RubricVersion); err != nil {
		return nil, translate(op+": insert event", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, translate(op+": commit", err)
	}
	return app, nil
}

// scoreApplication scores app against its post's current rubric and stores
// one scores row per criterion, inside tx. It returns the rubric version
// used, or nil if the post has no rubric, in which case nothing is stored.
//
// app.Answers must be the answers as stored, with their IDs: the engine
// copies an answer's ID into the score it was evidence for, and that ID is
// what evidence_answer_id points at.
//
// An error here fails the whole submission. A rubric the engine refuses is a
// broken rubric, and an application scored against part of one, or not at
// all while the post claims to have a rubric, is worse than no application.
func scoreApplication(ctx context.Context, tx pgx.Tx, app *domain.Application) (*int, error) {
	// The rubric cannot change while this runs: ReplaceRubric's lock waits
	// for the FOR SHARE that SubmitApplication holds on the post.
	rubric, err := loadRubric(ctx, tx, app.JobPostID)
	if err != nil {
		return nil, err
	}

	// Opening a post requires a weighted rubric, and a rubric cannot be
	// replaced by an empty one, so an open post without criteria means its
	// rows were changed by hand. The candidate is not at fault: the
	// application is kept, unscored, and rubric_version null in its event
	// says so. A rescore can score it once the rubric is repaired.
	if len(rubric.Criteria) == 0 {
		return nil, nil //nolint:nilnil // nil means "not scored", which is not an error.
	}

	// The engine sees domain values only: the criteria as loadRubric built
	// them and the answers as insertAnswers returned them.
	result, err := scoring.Score(app.Answers, rubric.Criteria)
	if err != nil {
		return nil, fmt.Errorf("score against rubric version %d: %w", rubric.Version, err)
	}

	if err = insertScores(ctx, tx, rubric.Version, []scoredApplication{{ID: app.ID, Breakdown: result.Breakdown}}); err != nil {
		return nil, err
	}
	return &rubric.Version, nil
}

// insertAnswers stores every answer of one application in a single statement
// and returns them in the order given.
//
// unnest turns the two arrays into rows, pairing element i of each, so N
// answers cost one round trip and one statement whatever N is. The SQL text
// never changes, so pgx prepares it once per connection and reuses it.
func insertAnswers(ctx context.Context, tx pgx.Tx, applicationID string, answers []domain.NewAnswer) ([]domain.Answer, error) {
	// Two parallel slices, because that is the shape unnest takes. pgx sends
	// a []string as a PostgreSQL text[].
	questionIDs := make([]string, len(answers))
	texts := make([]string, len(answers))
	for i, a := range answers {
		questionIDs[i] = a.QuestionID
		texts[i] = a.Answer
	}

	const query = `
		INSERT INTO application_answers (application_id, question_id, answer_text)
		SELECT $1::text::uuid, t.question_id::uuid, t.answer_text
		FROM unnest($2::text[], $3::text[]) AS t(question_id, answer_text)
		RETURNING id::text, application_id::text, question_id::text, answer_text, created_at`

	rows, err := tx.Query(ctx, query, applicationID, questionIDs, texts)
	if err != nil {
		return nil, translate("insert answers", err)
	}
	stored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Answer, error) {
		var a domain.Answer
		scanErr := row.Scan(&a.ID, &a.ApplicationID, &a.QuestionID, &a.Text, &a.CreatedAt)
		return a, scanErr
	})
	if err != nil {
		return nil, translate("insert answers", err)
	}

	// RETURNING promises no order, so the rows are put back in the order
	// they were submitted. Each question appears once, which validation and
	// the unique constraint both guarantee.
	position := make(map[string]int, len(answers))
	for i, a := range answers {
		position[a.QuestionID] = i
	}
	ordered := make([]domain.Answer, len(stored))
	for _, a := range stored {
		ordered[position[a.QuestionID]] = a
	}
	return ordered, nil
}

// scanApplication reads one row in applicationColumns order.
func scanApplication(row pgx.Row) (*domain.Application, error) {
	var a domain.Application

	err := row.Scan(&a.ID, &a.JobPostID, &a.CandidateID, &a.Status, &a.CVURL, &a.SubmittedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
