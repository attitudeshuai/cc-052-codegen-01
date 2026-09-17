package repository

import (
	"cc-052/internal/model"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type SampleRepo struct {
	db *sqlx.DB
}

func NewSampleRepo(db *sqlx.DB) *SampleRepo {
	return &SampleRepo{db: db}
}

const sampleColumns = `id, sample_no, plot_id, crop, sampler, sampled_at, quantity, status,
	cabinet, retain_until, disposed_at, disposal_method, disposed_by, void_reason, note, created_at`

func (r *SampleRepo) Create(s *model.Sample) error {
	query := `INSERT INTO sample (sample_no, plot_id, crop, sampler, sampled_at, quantity, status, cabinet, retain_until, note)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, created_at`
	return r.db.QueryRow(query, s.SampleNo, s.PlotID, s.Crop, s.Sampler, s.SampledAt,
		s.Quantity, s.Status, s.Cabinet, s.RetainUntil, s.Note).
		Scan(&s.ID, &s.CreatedAt)
}

func (r *SampleRepo) GetByID(id int64) (*model.Sample, error) {
	var s model.Sample
	query := `SELECT ` + sampleColumns + ` FROM sample WHERE id = $1`
	if err := r.db.Get(&s, query, id); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SampleRepo) GetBySampleNo(no string) (*model.Sample, error) {
	var s model.Sample
	query := `SELECT ` + sampleColumns + ` FROM sample WHERE sample_no = $1`
	if err := r.db.Get(&s, query, no); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SampleRepo) List(plotID int64, status string) ([]model.Sample, error) {
	query := `SELECT ` + sampleColumns + ` FROM sample WHERE 1=1`
	args := []interface{}{}
	if plotID > 0 {
		args = append(args, plotID)
		query += fmt.Sprintf(" AND plot_id = $%d", len(args))
	}
	if status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	query += " ORDER BY id DESC"
	var samples []model.Sample
	if err := r.db.Select(&samples, query, args...); err != nil {
		return nil, err
	}
	return samples, nil
}

func (r *SampleRepo) UpdateVoid(id int64, reason string) error {
	query := `UPDATE sample SET status = 'void', void_reason = $2 WHERE id = $1`
	_, err := r.db.Exec(query, id, reason)
	return err
}

func (r *SampleRepo) UpdateRetention(id int64, cabinet string, retainUntil time.Time) error {
	query := `UPDATE sample SET cabinet = $2, retain_until = $3 WHERE id = $1`
	_, err := r.db.Exec(query, id, cabinet, retainUntil)
	return err
}

func (r *SampleRepo) UpdateRetainUntil(id int64, retainUntil time.Time) error {
	query := `UPDATE sample SET retain_until = $2 WHERE id = $1`
	_, err := r.db.Exec(query, id, retainUntil)
	return err
}

func (r *SampleRepo) UpdateRetentionDisposed(id int64, method, operator string) error {
	query := `UPDATE sample SET disposed_at = NOW(), disposal_method = $2, disposed_by = $3 WHERE id = $1`
	_, err := r.db.Exec(query, id, method, operator)
	return err
}

// CreateTransfer 事务写入交接单 + 明细 + 样品状态变更
func (r *SampleRepo) CreateTransfer(t *model.SampleTransfer, items []model.SampleTransferItem, newStatus map[int64]model.SampleStatus) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = tx.QueryRow(`INSERT INTO sample_transfer (direction, out_transfer_id, lab, handler, happened_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		t.Direction, t.OutTransferID, t.Lab, t.Handler, t.HappenedAt).
		Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return err
	}

	stmt, err := tx.Preparex(`INSERT INTO sample_transfer_item (transfer_id, sample_id, quantity) VALUES ($1, $2, $3) RETURNING id`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := range items {
		items[i].TransferID = t.ID
		if err := stmt.QueryRow(t.ID, items[i].SampleID, items[i].Quantity).Scan(&items[i].ID); err != nil {
			return err
		}
	}

	for sampleID, status := range newStatus {
		if _, err := tx.Exec(`UPDATE sample SET status = $1 WHERE id = $2`, status, sampleID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *SampleRepo) GetTransferByID(id int64) (*model.SampleTransfer, error) {
	var t model.SampleTransfer
	query := `SELECT id, direction, out_transfer_id, lab, handler, happened_at, created_at
	          FROM sample_transfer WHERE id = $1`
	if err := r.db.Get(&t, query, id); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *SampleRepo) ListTransferItems(transferID int64) ([]model.SampleTransferItem, error) {
	var items []model.SampleTransferItem
	query := `SELECT i.id, i.transfer_id, i.sample_id, s.sample_no, i.quantity
	          FROM sample_transfer_item i
	          JOIN sample s ON s.id = i.sample_id
	          WHERE i.transfer_id = $1 ORDER BY i.id`
	if err := r.db.Select(&items, query, transferID); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SampleRepo) ListTransfersBySample(sampleID int64) ([]model.SampleTransfer, error) {
	var transfers []model.SampleTransfer
	query := `SELECT t.id, t.direction, t.out_transfer_id, t.lab, t.handler, t.happened_at, t.created_at
	          FROM sample_transfer t
	          JOIN sample_transfer_item i ON i.transfer_id = t.id
	          WHERE i.sample_id = $1
	          GROUP BY t.id
	          ORDER BY t.id`
	if err := r.db.Select(&transfers, query, sampleID); err != nil {
		return nil, err
	}
	return transfers, nil
}

// OutstandingQty 某样品在途未收回数量（累计送出 - 累计收回）
func (r *SampleRepo) OutstandingQty(sampleID int64) (int, error) {
	var qty int
	query := `SELECT COALESCE(SUM(CASE WHEN t.direction = 'out' THEN i.quantity ELSE -i.quantity END), 0)
	          FROM sample_transfer_item i
	          JOIN sample_transfer t ON t.id = i.transfer_id
	          WHERE i.sample_id = $1`
	if err := r.db.Get(&qty, query, sampleID); err != nil {
		return 0, err
	}
	return qty, nil
}

// SentQtyInTransfer 某送出单中某样品的送出数量
func (r *SampleRepo) SentQtyInTransfer(transferID, sampleID int64) (int, error) {
	var qty int
	query := `SELECT COALESCE(SUM(quantity), 0) FROM sample_transfer_item WHERE transfer_id = $1 AND sample_id = $2`
	if err := r.db.Get(&qty, query, transferID, sampleID); err != nil {
		return 0, err
	}
	return qty, nil
}

// ReturnedQtyForOutTransfer 针对某送出单，某样品已累计收回数量
func (r *SampleRepo) ReturnedQtyForOutTransfer(outTransferID, sampleID int64) (int, error) {
	var qty int
	query := `SELECT COALESCE(SUM(i.quantity), 0)
	          FROM sample_transfer_item i
	          JOIN sample_transfer t ON t.id = i.transfer_id
	          WHERE t.out_transfer_id = $1 AND i.sample_id = $2`
	if err := r.db.Get(&qty, query, outTransferID, sampleID); err != nil {
		return 0, err
	}
	return qty, nil
}

// ReconcileOutTransfer 对账：送出单每项明细的送出/已收回数量
func (r *SampleRepo) ReconcileOutTransfer(outTransferID int64) ([]model.ReconcileItem, error) {
	var items []model.ReconcileItem
	query := `SELECT i.sample_id, s.sample_no, SUM(i.quantity) AS sent_qty, COALESCE(r.returned_qty, 0) AS returned_qty
	          FROM sample_transfer_item i
	          JOIN sample s ON s.id = i.sample_id
	          LEFT JOIN (
	              SELECT ri.sample_id, SUM(ri.quantity) AS returned_qty
	              FROM sample_transfer_item ri
	              JOIN sample_transfer rt ON rt.id = ri.transfer_id
	              WHERE rt.out_transfer_id = $1
	              GROUP BY ri.sample_id
	          ) r ON r.sample_id = i.sample_id
	          WHERE i.transfer_id = $1
	          GROUP BY i.sample_id, s.sample_no, r.returned_qty
	          ORDER BY s.sample_no`
	if err := r.db.Select(&items, query, outTransferID); err != nil {
		return nil, err
	}
	return items, nil
}

// CreateConclusion 事务写入结论并更新样品状态为 concluded
func (r *SampleRepo) CreateConclusion(c *model.SampleConclusion) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = tx.QueryRow(`INSERT INTO sample_conclusion (sample_id, lab, result, concluded_at, report_url)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		c.SampleID, c.Lab, c.Result, c.ConcludedAt, c.ReportURL).
		Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sample SET status = $1 WHERE id = $2`, model.SampleStatusConcluded, c.SampleID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SampleRepo) GetConclusionBySample(sampleID int64) (*model.SampleConclusion, error) {
	var c model.SampleConclusion
	query := `SELECT id, sample_id, lab, result, concluded_at, report_url, created_at
	          FROM sample_conclusion WHERE sample_id = $1`
	if err := r.db.Get(&c, query, sampleID); err != nil {
		return nil, err
	}
	return &c, nil
}

// StatusCounts 按状态统计样品数（含作废，由 service 层剔除）
func (r *SampleRepo) StatusCounts() ([]model.SampleStatusCount, error) {
	var rows []model.SampleStatusCount
	query := `SELECT status, COUNT(*) AS count FROM sample GROUP BY status`
	if err := r.db.Select(&rows, query); err != nil {
		return nil, err
	}
	return rows, nil
}

// ResultCounts 结论统计（不含作废样品）
func (r *SampleRepo) ResultCounts() ([]model.SampleResultCount, error) {
	var rows []model.SampleResultCount
	query := `SELECT c.result, COUNT(*) AS count
	          FROM sample_conclusion c
	          JOIN sample s ON s.id = c.sample_id
	          WHERE s.status <> 'void'
	          GROUP BY c.result`
	if err := r.db.Select(&rows, query); err != nil {
		return nil, err
	}
	return rows, nil
}

// OutstandingTotalQty 全部有效样品的在途未收回数量（不含作废）
func (r *SampleRepo) OutstandingTotalQty() (int, error) {
	var qty int
	query := `SELECT COALESCE(SUM(CASE WHEN t.direction = 'out' THEN i.quantity ELSE -i.quantity END), 0)
	          FROM sample_transfer_item i
	          JOIN sample_transfer t ON t.id = i.transfer_id
	          JOIN sample s ON s.id = i.sample_id
	          WHERE s.status <> 'void'`
	if err := r.db.Get(&qty, query); err != nil {
		return 0, err
	}
	return qty, nil
}

// CountExpiredRetentions 到期未处理留样数（不含作废）
func (r *SampleRepo) CountExpiredRetentions() (int, error) {
	var n int
	query := `SELECT COUNT(*) FROM sample
	          WHERE status <> 'void' AND disposed_at IS NULL AND retain_until IS NOT NULL AND retain_until < CURRENT_DATE`
	if err := r.db.Get(&n, query); err != nil {
		return 0, err
	}
	return n, nil
}

// ListExpiredRetentions 到期未处理留样清单（不含作废）
func (r *SampleRepo) ListExpiredRetentions() ([]model.Sample, error) {
	var samples []model.Sample
	query := `SELECT ` + sampleColumns + ` FROM sample
	          WHERE status <> 'void' AND disposed_at IS NULL AND retain_until IS NOT NULL AND retain_until < CURRENT_DATE
	          ORDER BY retain_until ASC`
	if err := r.db.Select(&samples, query); err != nil {
		return nil, err
	}
	return samples, nil
}
