package repository

import (
	"cc-052/internal/model"

	"github.com/jmoiron/sqlx"
)

type CustodyRepo struct {
	db *sqlx.DB
}

func NewCustodyRepo(db *sqlx.DB) *CustodyRepo {
	return &CustodyRepo{db: db}
}

const custodyColumns = `id, sample_id, direction, quantity, handler_from, handler_to, transferred_at, note, created_at`

func (r *CustodyRepo) Create(c *model.SampleCustody) error {
	query := `INSERT INTO sample_custody
		(sample_id, direction, quantity, handler_from, handler_to, transferred_at, note)
		VALUES (:sample_id, :direction, :quantity, :handler_from, :handler_to, :transferred_at, :note)
		RETURNING id, created_at`
	stmt, err := r.db.PrepareNamed(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	return stmt.QueryRowx(c).Scan(&c.ID, &c.CreatedAt)
}

func (r *CustodyRepo) ListBySample(sampleID int64) ([]model.SampleCustody, error) {
	var rows []model.SampleCustody
	query := `SELECT ` + custodyColumns + ` FROM sample_custody
	          WHERE sample_id = $1 ORDER BY transferred_at ASC, id ASC`
	if err := r.db.Select(&rows, query, sampleID); err != nil {
		return nil, err
	}
	return rows, nil
}

// Sums 累计交出 / 收回数量
func (r *CustodyRepo) Sums(sampleID int64) (sent, returned int, err error) {
	query := `SELECT
		COALESCE(SUM(quantity) FILTER (WHERE direction = 'send'), 0),
		COALESCE(SUM(quantity) FILTER (WHERE direction = 'return'), 0)
		FROM sample_custody WHERE sample_id = $1`
	err = r.db.QueryRowx(query, sampleID).Scan(&sent, &returned)
	return
}

// Shortages 当场点名：按批次列出"交出去的没收回来 / 收回来的比交出多"的样品。
// batchID 为 0 时对全部未结清样品点名。
func (r *CustodyRepo) Shortages(batchID int64) ([]model.CustodyShortage, error) {
	query := `
		SELECT s.sample_no,
		       COALESCE(SUM(c.quantity) FILTER (WHERE c.direction = 'send'), 0)   AS sent_quantity,
		       COALESCE(SUM(c.quantity) FILTER (WHERE c.direction = 'return'), 0) AS returned_quantity,
		       COALESCE(SUM(c.quantity) FILTER (WHERE c.direction = 'send'), 0)
		     - COALESCE(SUM(c.quantity) FILTER (WHERE c.direction = 'return'), 0) AS missing_quantity
		FROM sample s
		JOIN sample_custody c ON c.sample_id = s.id
		WHERE ($1 = 0 OR s.batch_id = $1)
		  AND s.status <> 'void'
		GROUP BY s.id, s.sample_no
		HAVING COALESCE(SUM(c.quantity) FILTER (WHERE c.direction = 'send'), 0)
		     - COALESCE(SUM(c.quantity) FILTER (WHERE c.direction = 'return'), 0) <> 0
		ORDER BY missing_quantity DESC, s.sample_no ASC`
	var rows []model.CustodyShortage
	if err := r.db.Select(&rows, query, batchID); err != nil {
		return nil, err
	}
	return rows, nil
}
