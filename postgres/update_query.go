package sq

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// UpdateQuery represents an UPDATE query.
type UpdateQuery struct {
	nested bool
	// WITH
	CTEs []CTE
	// UPDATE
	UpdateTable BaseTable
	// SET
	Assignments Assignments
	// FROM
	FromTable  Table
	JoinTables JoinTables
	// WHERE
	WherePredicate VariadicPredicate
	// RETURNING
	ReturningFields Fields
	// DB
	DB           DB
	ColumnMapper func(*Column)
	RowMapper    func(*Row)
	Accumulator  func()
	// Logging
	Log     Logger
	LogFlag LogFlag
	logSkip int
}

func (q UpdateQuery) ToSQL() (query string, args []interface{}) {
	defer func() {
		if r := recover(); r != nil {
			args = []interface{}{r}
		}
	}()
	q.logSkip += 1
	buf := &strings.Builder{}
	q.AppendSQL(buf, &args, nil)
	return buf.String(), args
}

// AppendSQL marshals the UpdateQuery into a buffer and args slice. Do not call
// this as an end user, use ToSQL instead. AppendSQL may panic if you wrote
// panic code in your ColumnMapper, it is only exported to satisfy the Query
// interface.
func (q UpdateQuery) AppendSQL(buf *strings.Builder, args *[]interface{}, params map[string]int) {
	var excludedTableQualifiers []string
	if q.ColumnMapper != nil {
		col := &Column{mode: colmodeUpdate}
		q.ColumnMapper(col)
		q.Assignments = col.assignments
	}
	// WITH
	if !q.nested {
		appendCTEs(buf, args, q.CTEs, q.FromTable, q.JoinTables)
	}
	// UPDATE
	buf.WriteString("UPDATE ")
	if q.UpdateTable == nil {
		buf.WriteString("NULL")
	} else {
		q.UpdateTable.AppendSQL(buf, args, nil)
		name := q.UpdateTable.GetName()
		alias := q.UpdateTable.GetAlias()
		if alias != "" {
			buf.WriteString(" AS ")
			buf.WriteString(alias)
			excludedTableQualifiers = append(excludedTableQualifiers, alias)
		} else {
			excludedTableQualifiers = append(excludedTableQualifiers, name)
		}
	}
	// SET
	if len(q.Assignments) > 0 {
		buf.WriteString(" SET ")
		q.Assignments.AppendSQLExclude(buf, args, nil, excludedTableQualifiers)
	}
	// FROM
	if q.FromTable != nil {
		buf.WriteString(" FROM ")
		switch v := q.FromTable.(type) {
		case Query:
			buf.WriteString("(")
			v.NestThis().AppendSQL(buf, args, nil)
			buf.WriteString(")")
		default:
			q.FromTable.AppendSQL(buf, args, nil)
		}
		alias := q.FromTable.GetAlias()
		if alias != "" {
			buf.WriteString(" AS ")
			buf.WriteString(alias)
		}
	}
	// JOIN
	if len(q.JoinTables) > 0 {
		buf.WriteString(" ")
		q.JoinTables.AppendSQL(buf, args, nil)
	}
	// WHERE
	if len(q.WherePredicate.Predicates) > 0 {
		buf.WriteString(" WHERE ")
		q.WherePredicate.toplevel = true
		q.WherePredicate.AppendSQLExclude(buf, args, nil, nil)
	}
	// RETURNING
	if len(q.ReturningFields) > 0 {
		buf.WriteString(" RETURNING ")
		q.ReturningFields.AppendSQLExcludeWithAlias(buf, args, nil, nil)
	}
	if !q.nested {
		query := buf.String()
		buf.Reset()
		questionToDollarPlaceholders(buf, query)
		if q.Log != nil {
			var logOutput string
			switch {
			case Lstats&q.LogFlag != 0:
				logOutput = "\n----[ Executing query ]----\n" + buf.String() + " " + fmt.Sprint(*args) +
					"\n----[ with bind values ]----\n" + questionInterpolate(query, *args...)
			case Linterpolate&q.LogFlag != 0:
				logOutput = questionInterpolate(query, *args...)
			default:
				logOutput = buf.String() + " " + fmt.Sprint(*args)
			}
			switch q.Log.(type) {
			case *log.Logger:
				_ = q.Log.Output(q.logSkip+2, logOutput)
			default:
				_ = q.Log.Output(q.logSkip+1, logOutput)
			}
		}
	}
}

// Update creates a new UpdateQuery.
func Update(table BaseTable) UpdateQuery {
	return UpdateQuery{
		UpdateTable: table,
	}
}

// With appends a list of CTEs into the UpdateQuery.
func (q UpdateQuery) With(ctes ...CTE) UpdateQuery {
	q.CTEs = append(q.CTEs, ctes...)
	return q
}

// Update sets the update table for the UpdateQuery.
func (q UpdateQuery) Update(table BaseTable) UpdateQuery {
	q.UpdateTable = table
	return q
}

// Set appends the assignments to SET clause of the UpdateQuery.
func (q UpdateQuery) Set(assignments ...Assignment) UpdateQuery {
	q.Assignments = append(q.Assignments, assignments...)
	return q
}

// Setx sets the column mapper function UpdateQuery.
func (q UpdateQuery) Setx(mapper func(*Column)) UpdateQuery {
	q.ColumnMapper = mapper
	return q
}

// From specifies a table to select from for the purposes of the UpdateQuery.
func (q UpdateQuery) From(table Table) UpdateQuery {
	q.FromTable = table
	return q
}

// Join joins a new table to the UpdateQuery based on the predicates.
func (q UpdateQuery) Join(table Table, predicate Predicate, predicates ...Predicate) UpdateQuery {
	predicates = append([]Predicate{predicate}, predicates...)
	q.JoinTables = append(q.JoinTables, JoinTable{
		JoinType: JoinTypeInner,
		Table:    table,
		OnPredicates: VariadicPredicate{
			Predicates: predicates,
		},
	})
	return q
}

// LeftJoin left joins a new table to the UpdateQuery based on the predicates.
func (q UpdateQuery) LeftJoin(table Table, predicate Predicate, predicates ...Predicate) UpdateQuery {
	predicates = append([]Predicate{predicate}, predicates...)
	q.JoinTables = append(q.JoinTables, JoinTable{
		JoinType: JoinTypeLeft,
		Table:    table,
		OnPredicates: VariadicPredicate{
			Predicates: predicates,
		},
	})
	return q
}

// RightJoin right joins a new table to the UpdateQuery based on the predicates.
func (q UpdateQuery) RightJoin(table Table, predicate Predicate, predicates ...Predicate) UpdateQuery {
	predicates = append([]Predicate{predicate}, predicates...)
	q.JoinTables = append(q.JoinTables, JoinTable{
		JoinType: JoinTypeRight,
		Table:    table,
		OnPredicates: VariadicPredicate{
			Predicates: predicates,
		},
	})
	return q
}

// FullJoin full joins a table to the UpdateQuery based on the predicates.
func (q UpdateQuery) FullJoin(table Table, predicate Predicate, predicates ...Predicate) UpdateQuery {
	predicates = append([]Predicate{predicate}, predicates...)
	q.JoinTables = append(q.JoinTables, JoinTable{
		JoinType: JoinTypeFull,
		Table:    table,
		OnPredicates: VariadicPredicate{
			Predicates: predicates,
		},
	})
	return q
}

// CustomJoin custom joins a table to the UpdateQuery. The join type can be
// specified with a string, e.g. "CROSS JOIN".
func (q UpdateQuery) CustomJoin(joinType JoinType, table Table, predicates ...Predicate) UpdateQuery {
	q.JoinTables = append(q.JoinTables, JoinTable{
		JoinType: joinType,
		Table:    table,
		OnPredicates: VariadicPredicate{
			Predicates: predicates,
		},
	})
	return q
}

// Where appends the predicates to the WHERE clause in the UpdateQuery.
func (q UpdateQuery) Where(predicates ...Predicate) UpdateQuery {
	q.WherePredicate.Predicates = append(q.WherePredicate.Predicates, predicates...)
	return q
}

// Returning appends the fields to the RETURNING clause of the InsertQuery.
func (q UpdateQuery) Returning(fields ...Field) UpdateQuery {
	q.ReturningFields = append(q.ReturningFields, fields...)
	return q
}

// ReturningOne sets the RETURNING clause to RETURNING 1 in the InsertQuery.
func (q UpdateQuery) ReturningOne() UpdateQuery {
	q.ReturningFields = Fields{FieldLiteral("1")}
	return q
}

// Returningx sets the rowmapper and accumulator function of the InsertQuery.
func (q UpdateQuery) Returningx(mapper func(*Row), accumulator func()) UpdateQuery {
	q.RowMapper = mapper
	q.Accumulator = accumulator
	return q
}

// ReturningRowx sets the rowmapper function of the InsertQuery.
func (q UpdateQuery) ReturningRowx(mapper func(*Row)) UpdateQuery {
	q.RowMapper = mapper
	return q
}

// Fetch will run UpdateQuery with the given DB. It then maps the results based
// on the mapper function (and optionally runs the accumulator function).
func (q UpdateQuery) Fetch(db DB) (err error) {
	q.logSkip += 1
	return q.FetchContext(nil, db)
}

// FetchContext will run UpdateQuery with the given DB and context. It then
// maps the results based on the mapper function (and optionally runs the
// accumulator function).
func (q UpdateQuery) FetchContext(ctx context.Context, db DB) (err error) {
	if db == nil {
		if q.DB == nil {
			return errors.New("DB cannot be nil")
		}
		db = q.DB
	}
	if q.RowMapper == nil {
		return fmt.Errorf("cannot call Fetch/FetchContext without a mapper")
	}
	logBuf := &strings.Builder{}
	start := time.Now()
	var rowcount int
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case ExitCode:
				if v != ExitPeacefully {
					err = v
				}
			case error:
				err = v
			default:
				err = fmt.Errorf("%#v", r)
			}
			return
		}
		if q.Log == nil {
			return
		}
		elapsed := time.Since(start)
		if Lresults&q.LogFlag != 0 && rowcount > 5 {
			logBuf.WriteString("\n...")
		}
		if Lstats&q.LogFlag != 0 {
			logBuf.WriteString("\n(Fetched ")
			logBuf.WriteString(strconv.Itoa(rowcount))
			logBuf.WriteString(" rows in ")
			logBuf.WriteString(elapsed.String())
			logBuf.WriteString(")")
		}
		if logBuf.Len() > 0 {
			switch q.Log.(type) {
			case *log.Logger:
				_ = q.Log.Output(q.logSkip+2, logBuf.String())
			default:
				_ = q.Log.Output(q.logSkip+1, logBuf.String())
			}
		}
	}()
	r := &Row{}
	q.RowMapper(r)
	q.ReturningFields = r.fields
	tmpbuf := &strings.Builder{}
	var tmpargs []interface{}
	q.logSkip += 1
	q.AppendSQL(tmpbuf, &tmpargs, nil)
	if ctx == nil {
		r.rows, err = db.Query(tmpbuf.String(), tmpargs...)
	} else {
		r.rows, err = db.QueryContext(ctx, tmpbuf.String(), tmpargs...)
	}
	if err != nil {
		return err
	}
	defer r.rows.Close()
	if len(r.dest) == 0 {
		return nil
	}
	for r.rows.Next() {
		rowcount++
		err = r.rows.Scan(r.dest...)
		if err != nil {
			errbuf := &strings.Builder{}
			for i := range r.dest {
				tmpbuf.Reset()
				tmpargs = tmpargs[:0]
				r.fields[i].AppendSQLExclude(tmpbuf, &tmpargs, nil, nil)
				errbuf.WriteString("\n" +
					strconv.Itoa(i) + ") " +
					dollarInterpolate(tmpbuf.String(), tmpargs...) + " => " +
					reflect.TypeOf(r.dest[i]).String())
			}
			return fmt.Errorf("Please check if your mapper function is correct:%s\n%w", errbuf.String(), err)
		}
		if q.Log != nil && Lresults&q.LogFlag != 0 && rowcount <= 5 {
			logBuf.WriteString("\n----[ Row ")
			logBuf.WriteString(strconv.Itoa(rowcount))
			logBuf.WriteString(" ]----")
			for i := range r.dest {
				tmpbuf.Reset()
				tmpargs = tmpargs[:0]
				r.fields[i].AppendSQLExclude(tmpbuf, &tmpargs, nil, nil)
				logBuf.WriteString("\n")
				logBuf.WriteString(dollarInterpolate(tmpbuf.String(), tmpargs...))
				logBuf.WriteString(": ")
				logBuf.WriteString(appendSQLDisplay(r.dest[i]))
			}
		}
		r.index = 0
		q.RowMapper(r)
		if q.Accumulator == nil {
			break
		}
		q.Accumulator()
	}
	if rowcount == 0 && q.Accumulator == nil {
		return sql.ErrNoRows
	}
	if e := r.rows.Close(); e != nil {
		return e
	}
	return r.rows.Err()
}

// Exec will execute the UpdateQuery with the given DB. It will only compute
// the rowsAffected if the ErowsAffected Execflag is passed to it.
func (q UpdateQuery) Exec(db DB, flag ExecFlag) (rowsAffected int64, err error) {
	q.logSkip += 1
	return q.ExecContext(nil, db, flag)
}

// ExecContext will execute the UpdateQuery with the given DB and context. It will
// only compute the rowsAffected if the ErowsAffected Execflag is passed to it.
func (q UpdateQuery) ExecContext(ctx context.Context, db DB, flag ExecFlag) (rowsAffected int64, err error) {
	if db == nil {
		if q.DB == nil {
			return rowsAffected, errors.New("DB cannot be nil")
		}
		db = q.DB
	}
	logBuf := &strings.Builder{}
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case error:
				err = v
			default:
				err = fmt.Errorf("%#v", r)
			}
			return
		}
		if q.Log == nil {
			return
		}
		elapsed := time.Since(start)
		if Lstats&q.LogFlag != 0 && ErowsAffected&flag != 0 {
			logBuf.WriteString("\n(Updated ")
			logBuf.WriteString(strconv.FormatInt(rowsAffected, 10))
			logBuf.WriteString(" rows in ")
			logBuf.WriteString(elapsed.String())
			logBuf.WriteString(")")
		}
		if logBuf.Len() > 0 {
			switch q.Log.(type) {
			case *log.Logger:
				_ = q.Log.Output(q.logSkip+2, logBuf.String())
			default:
				_ = q.Log.Output(q.logSkip+1, logBuf.String())
			}
		}
	}()
	var res sql.Result
	tmpbuf := &strings.Builder{}
	var tmpargs []interface{}
	q.logSkip += 1
	q.AppendSQL(tmpbuf, &tmpargs, nil)
	if ctx == nil {
		res, err = db.Exec(tmpbuf.String(), tmpargs...)
	} else {
		res, err = db.ExecContext(ctx, tmpbuf.String(), tmpargs...)
	}
	if err != nil {
		return rowsAffected, err
	}
	if res != nil && ErowsAffected&flag != 0 {
		rowsAffected, err = res.RowsAffected()
		if err != nil {
			return rowsAffected, err
		}
	}
	return rowsAffected, nil
}

// NestThis indicates to the UpdateQuery that it is nested.
func (q UpdateQuery) NestThis() Query {
	q.nested = true
	return q
}
