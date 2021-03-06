package sq

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/matryer/is"
)

func TestSelectQuery_ToSQL(t *testing.T) {
	type TT struct {
		description string
		q           SelectQuery
		wantQuery   string
		wantArgs    []interface{}
	}
	u := USERS().As("u")
	tests := []TT{
		{"empty", SelectQuery{}, "SELECT", nil},
		{"From", Select().From(u), "SELECT FROM devlab.users AS u", nil},
		{"SelectOne", Select().SelectOne().From(u), "SELECT 1 FROM devlab.users AS u", nil},
		{"SelectDistinct", Select().SelectDistinct(u.USER_ID).From(u), "SELECT DISTINCT u.user_id FROM devlab.users AS u", nil},
		{
			"Joins",
			From(SelectOne().From(u).Subquery("subquery")).
				Join(u, u.USER_ID.Eq(u.USER_ID)).
				LeftJoin(u, u.USER_ID.Eq(u.USER_ID)).
				RightJoin(u, u.USER_ID.Eq(u.USER_ID)).
				FullJoin(u, u.USER_ID.Eq(u.USER_ID)).
				CustomJoin("CROSS JOIN", u),
			"SELECT FROM (SELECT 1 FROM devlab.users AS u) AS subquery" +
				" JOIN devlab.users AS u ON u.user_id = u.user_id" +
				" LEFT JOIN devlab.users AS u ON u.user_id = u.user_id" +
				" RIGHT JOIN devlab.users AS u ON u.user_id = u.user_id" +
				" FULL JOIN devlab.users AS u ON u.user_id = u.user_id" +
				" CROSS JOIN devlab.users AS u",
			nil,
		},
		func() TT {
			desc := "assorted"
			w1 := PartitionBy(u.DISPLAYNAME).OrderBy(u.EMAIL).As("w1")
			w2 := OrderBy(u.PASSWORD).As("w2")
			cte1 := SelectOne().From(u).CTE("cte1")
			cte2 := SelectDistinct(u.EMAIL).From(u).CTE("cte2")
			q := WithDefaultLog(Lverbose).
				Select(
					SumOver(u.USER_ID, PartitionBy(u.DISPLAYNAME).OrderBy(u.EMAIL)),
					SumOver(u.USER_ID, w1),
					SumOver(u.USER_ID, w1.Name()),
				).
				From(SelectDistinct(u.USER_ID).From(u).Subquery("subquery")).
				CustomJoin("NATURAL JOIN", cte1).
				CustomJoin("NATURAL JOIN", cte2).
				Where(u.USER_ID.EqInt(1), u.DISPLAYNAME.Eq(u.PASSWORD)).
				GroupBy(u.USER_ID, u.PASSWORD, u.DISPLAYNAME).
				Having(u.USER_ID.GtInt(3), u.EMAIL.LikeString("%gmail%")).
				Window(w1, w2).
				OrderBy(u.PASSWORD, u.DISPLAYNAME.Desc()).
				Limit(10).
				Offset(20)
			wantQuery := "WITH cte1 AS (SELECT 1 FROM devlab.users AS u)" +
				", cte2 AS (SELECT DISTINCT u.email FROM devlab.users AS u)" +
				" SELECT" +
				" SUM(u.user_id) OVER (PARTITION BY u.displayname ORDER BY u.email)" +
				", SUM(u.user_id) OVER (PARTITION BY u.displayname ORDER BY u.email)" +
				", SUM(u.user_id) OVER w1" +
				" FROM (SELECT DISTINCT u.user_id FROM devlab.users AS u) AS subquery" +
				" NATURAL JOIN cte1" +
				" NATURAL JOIN cte2" +
				" WHERE u.user_id = ? AND u.displayname = u.password" +
				" GROUP BY u.user_id, u.password, u.displayname" +
				" HAVING u.user_id > ? AND u.email LIKE ?" +
				" WINDOW w1 AS (PARTITION BY u.displayname ORDER BY u.email)" +
				", w2 AS (ORDER BY u.password)" +
				" ORDER BY u.password, u.displayname DESC" +
				" LIMIT ?" +
				" OFFSET ?"
			wantArgs := []interface{}{1, 3, "%gmail%", int64(10), int64(20)}
			return TT{desc, q, wantQuery, wantArgs}
		}(),
		{
			"negative limit and offset get abs'd",
			Select().Limit(-10).Offset(-20),
			"SELECT LIMIT ? OFFSET ?",
			[]interface{}{int64(10), int64(20)},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			is := is.New(t)
			var _ Query = tt.q
			gotQuery, gotArgs := tt.q.ToSQL()
			is.Equal(tt.wantQuery, gotQuery)
			is.Equal(tt.wantArgs, gotArgs)
		})
	}
}

type User struct {
	Valid       bool
	UserID      int
	Displayname string
	Email       string
	Password    string
}

func (u *User) RowMapper(tbl TABLE_USERS) func(*Row) {
	return func(row *Row) {
		*u = User{
			Valid:       row.IntValid(tbl.USER_ID),
			UserID:      row.Int(tbl.USER_ID),
			Displayname: row.String(tbl.DISPLAYNAME),
			Email:       row.String(tbl.EMAIL),
			Password:    row.String(tbl.PASSWORD),
		}
	}
}

func TestSelectQuery_Fetch(t *testing.T) {
	if testing.Short() {
		return
	}
	is := is.New(t)
	db, err := sql.Open("txdb", "SelectQuery_Fetch")
	is.NoErr(err)
	defer db.Close()
	u := USERS()

	// Missing DB
	err = From(u).
		Where(u.USER_ID.EqInt(1)).
		SelectRowx(func(row *Row) {}).
		Fetch(nil)
	is.True(err != nil)

	// SQL syntax error
	// use a tempDB so we don't foul up the current db transaction with the error
	tempDB, err := sql.Open("txdb", randomString(8))
	is.NoErr(err)
	var uid int
	err = WithDefaultLog(Linterpolate).
		WithDB(tempDB).
		From(u).
		Where(u.USER_ID.EqInt(1)).
		SelectRowx(func(row *Row) {
			row.ScanInto(&uid, u.USER_ID.Asc())
		}).
		Fetch(nil)
	is.True(err != nil)
	tempDB.Close()

	// No mapper
	err = WithDB(db).
		From(u).
		Fetch(nil)
	is.True(err != nil)

	// Empty mapper
	err = WithDefaultLog(Lverbose).WithDB(db).
		From(u).
		Where(u.USER_ID.EqInt(1)).
		SelectRowx(func(row *Row) {}).
		Fetch(nil)
	is.NoErr(err)

	// sql.ErrNoRows
	err = WithDefaultLog(Lverbose).
		WithDB(db).
		From(u).
		Where(u.USER_ID.EqInt(-999999)).
		SelectRowx(func(row *Row) {
			row.Int(u.USER_ID)
		}).
		Fetch(nil)
	is.True(errors.Is(err, sql.ErrNoRows))

	// simulate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	err = WithDefaultLog(Lverbose).
		WithDB(db).
		From(u).
		OrderBy(u.USER_ID).
		SelectRowx(func(row *Row) {}).
		FetchContext(ctx, nil)
	is.True(errors.Is(err, context.DeadlineExceeded))

	// Accumulator
	user := &User{}
	var users []User
	err = WithDefaultLog(Lverbose).
		WithDB(db).
		From(u).
		OrderBy(u.USER_ID).
		Limit(10).
		Selectx(user.RowMapper(u), func() { users = append(users, *user) }).
		Fetch(nil)
	is.NoErr(err)
	is.Equal(10, len(users))

	// Panic with ExitPeacefully
	users = users[:0]
	err = WithDefaultLog(Linterpolate).
		WithDB(db).
		From(u).
		OrderBy(u.USER_ID).
		Limit(10).
		Selectx(user.RowMapper(u), func() { panic(ExitPeacefully) }).
		Fetch(nil)
	is.NoErr(err)
	is.Equal(0, len(users))

	// Panic with any other ExitCode
	users = users[:0]
	err = WithDefaultLog(Linterpolate).
		WithDB(db).
		From(u).
		OrderBy(u.USER_ID).
		Limit(10).
		Selectx(user.RowMapper(u), func() { panic(ExitCode(1)) }).
		Fetch(nil)
	is.True(errors.Is(err, ExitCode(1)))

	// Panic with error
	ErrTest := errors.New("this is a test error")
	users = users[:0]
	err = WithDefaultLog(Linterpolate).
		WithDB(db).
		From(u).
		OrderBy(u.USER_ID).
		Limit(10).
		Selectx(user.RowMapper(u), func() { panic(ErrTest) }).
		Fetch(nil)
	is.True(errors.Is(err, ErrTest))

	// Panic with 0
	users = users[:0]
	err = WithDefaultLog(Linterpolate).
		WithDB(db).
		From(u).
		OrderBy(u.USER_ID).
		Limit(10).
		Selectx(user.RowMapper(u), func() { panic(0) }).
		Fetch(nil)
	is.Equal(fmt.Errorf("0").Error(), err.Error())
}

func TestSelectQuery_Basic(t *testing.T) {
	is := is.New(t)

	var q SelectQuery
	q = Selectx(nil, nil)
	is.Equal(nil, q.RowMapper)
	is.Equal(nil, q.Accumulator)

	q = SelectRowx(nil)
	is.Equal(nil, q.RowMapper)
}
