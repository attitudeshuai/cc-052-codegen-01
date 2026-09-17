package repository

import (
	"cc-052/internal/model"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type ConclusionRepo struct {
	db *sqlx.DB
}

func NewConclusionRepo(db *sqlx.DB) *ConclusionRepo {
	return &ConclusionRepo{db: db}
}

const conclusionColumns = `id, sample_id, lab, result, concluded_at, report_no, report_url, items,
	is_active, invalidate_reason, created_at`

func (r *ConclusionRepo) Create(c *model.SampleConclusion) error {
	query := `INSERT INTO sample_conclusion
		(sample_id, lab, result, concluded_at, report_no, report_url, items, is_active)
		VALUES (:sample_id, :lab, :result, :concluded_at, :report_no, :report_url, :items, :is_active)
		RETURNING id, created_at`
	stmt, err := r.db.PrepareNamed(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	return stmt.QueryRowx(c).Scan(&c.ID, &c.CreatedAt)
}

func (r *ConclusionRepo) GetByID(id int64) (*model.SampleConclusion, error) {
	var c model.SampleConclusion
	query := `SELECT ` + conclusionColumns + ` FROM sample_conclusion WHERE id = $1`
	if err := r.db.Get(&c, query, id); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ConclusionRepo) GetActiveBySample(sampleID int64) (*model.SampleConclusion, error) {
	var c model.SampleConclusion
	query := `SELECT ` + conclusionColumns + ` FROM sample_conclusion
	          WHERE sample_id = $1 AND is_active`
	if err := r.db.Get(&c, query, sampleID); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ConclusionRepo) GetActiveByReportNo(reportNo string) (*model.SampleConclusion, error) {
	var c model.SampleConclusion
	query := `SELECT ` + conclusionColumns + ` FROM sample_conclusion WHERE report_no = $1 AND is_active`
	if err := r.db.Get(&c, query, reportNo); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ConclusionRepo) ListBySample(sampleID int64) ([]model.SampleConclusion, error) {
	var rows []model.SampleConclusion
	query := `SELECT ` + conclusionColumns + ` FROM sample_conclusion
	          WHERE sample_id = $1 ORDER BY created_at DESC, id DESC`
	if err := r.db.Select(&rows, query, sampleID); err != nil {
		return nil, err
	}
	return rows, nil
}

// Deactivate 把挂错/作废的结论判为无效，立刻剔除统计
func (r *ConclusionRepo) Deactivate(id int64, reason string) error {
	res, err := r.db.Exec(
		`UPDATE sample_conclusion SET is_active = FALSE, invalidate_reason = $1
		 WHERE id = $2 AND is_active`,
		reason, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeactivateBySample 作废样品时把其有效结论一并剔除统计
func (r *ConclusionRepo) DeactivateBySample(sampleID int64, reason string) error {
	_, err := r.db.Exec(
		`UPDATE sample_conclusion SET is_active = FALSE, invalidate_reason = $1
		 WHERE sample_id = $2 AND is_active`,
		reason, sampleID)
	return err
}

// ConclusionCounters 台账统计原始计数（作废样品不进入统计）
type ConclusionCounters struct {
	Total     int64 `db:"total"`
	InLab     int64 `db:"in_lab"`
	Voided    int64 `db:"voided"`
	Concluded int64 `db:"concluded"`
	Passed    int64 `db:"passed"`
	Failed    int64 `db:"failed"`
}

func (r *ConclusionRepo) Counters() (*ConclusionCounters, error) {
	var c ConclusionCounters
	query := `
		SELECT
			COUNT(*) FILTER (WHERE s.status <> 'void')                          AS total,
			COUNT(*) FILTER (WHERE s.status = 'in_lab')                         AS in_lab,
			COUNT(*) FILTER (WHERE s.status = 'void')                            AS voided,
			COUNT(c.id) FILTER (WHERE s.status <> 'void')                       AS concluded,
			COUNT(c.id) FILTER (WHERE s.status <> 'void' AND c.result = 'pass') AS passed,
			COUNT(c.id) FILTER (WHERE s.status <> 'void' AND c.result = 'fail') AS failed
		FROM sample s
		LEFT JOIN sample_conclusion c ON c.sample_id = s.id AND c.is_active`
	if err := r.db.Get(&c, query); err != nil {
		return nil, err
	}
	return &c, nil
}
