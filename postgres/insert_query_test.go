package sq

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/matryer/is"
)

func TestInsertQuery_ToSQL(t *testing.T) {
	type TT struct {
		description string
		q           InsertQuery
		wantQuery   string
		wantArgs    []interface{}
	}
	u := USERS().As("u")
	tests := []TT{
		{"empty", InsertQuery{}, "INSERT INTO NULL", nil},
		{"Into", WithDefaultLog(Linterpolate).InsertInto(u), "INSERT INTO public.users AS u", nil},
		{
			"Insert Values",
			WithDefaultLog(Lverbose).
				InsertInto(u).
				Columns(u.DISPLAYNAME, u.EMAIL).
				Values("aaa", "aaa@email.com").
				Values("bbb", "bbb@email.com").
				OnConflict().DoNothing().
				ReturningOne(),
			"INSERT INTO public.users AS u (displayname, email)" +
				" VALUES ($1, $2), ($3, $4)" +
				" ON CONFLICT DO NOTHING" +
				" RETURNING 1",
			[]interface{}{"aaa", "aaa@email.com", "bbb", "bbb@email.com"},
		},
		func() TT {
			var tt TT
			tt.description = "Insert Select"
			cte1 := SelectOne().From(u).CTE("cte1")
			cte2 := SelectDistinct(u.EMAIL).From(u).CTE("cte2")
			tt.q = WithDefaultLog(0).
				InsertInto(u).
				Columns(u.DISPLAYNAME, u.EMAIL).
				Select(
					Select(u.DISPLAYNAME, u.EMAIL).
						From(u).
						CustomJoin("NATURAL JOIN", cte1).
						CustomJoin("NATURAL JOIN", cte2).
						Where(u.USER_ID.In([]int{1, 2, 3})),
				).
				OnConflict(u.DISPLAYNAME, u.EMAIL).
				Where(u.DISPLAYNAME.IsNotNull()).
				DoUpdateSet(
					u.DISPLAYNAME.Set(Excluded(u.DISPLAYNAME)),
					u.EMAIL.Set(Excluded(u.EMAIL)),
				).
				Where(u.EMAIL.IsNotNull()).
				Returning(u.DISPLAYNAME, u.EMAIL)
			tt.wantQuery = "WITH cte1 AS (SELECT 1 FROM public.users AS u)" +
				", cte2 AS (SELECT DISTINCT u.email FROM public.users AS u)" +
				" INSERT INTO public.users AS u (displayname, email)" +
				" SELECT u.displayname, u.email FROM public.users AS u" +
				" NATURAL JOIN cte1" +
				" NATURAL JOIN cte2" +
				" WHERE u.user_id IN ($1, $2, $3)" +
				" ON CONFLICT (displayname, email)" +
				" WHERE displayname IS NOT NULL" +
				" DO UPDATE SET" +
				" displayname = EXCLUDED.displayname, email = EXCLUDED.email" +
				" WHERE u.email IS NOT NULL" +
				" RETURNING u.displayname, u.email"
			tt.wantArgs = []interface{}{1, 2, 3}
			return tt
		}(),
		func() TT {
			desc := "aliasless table"
			u := USERS()
			q := WithDefaultLog(0).InsertInto(u).Columns(u.DISPLAYNAME, u.EMAIL)
			wantQuery := "INSERT INTO public.users (displayname, email)"
			return TT{desc, q, wantQuery, nil}
		}(),
		func() TT {
			var tt TT
			tt.description = "Valuesx One Entry"
			user := User{
				Displayname: "Bob",
				Email:       "bob@email.com",
				Password:    "cant_hack_me",
			}
			u := USERS().As("u")
			tt.q = WithDefaultLog(Lverbose).
				InsertInto(u).
				Valuesx(func(col *Column) {
					col.SetString(u.DISPLAYNAME, user.Displayname)
					col.SetString(u.EMAIL, user.Email)
					col.SetString(u.PASSWORD, user.Password)
				}).
				Returning(u.USER_ID)
			tt.wantQuery = "INSERT INTO public.users AS u (displayname, email, password)" +
				" VALUES ($1, $2, $3)" +
				" RETURNING u.user_id"
			tt.wantArgs = []interface{}{user.Displayname, user.Email, user.Password}
			return tt
		}(),
		func() TT {
			var tt TT
			tt.description = "Valuesx Multiple Entries"
			users := []User{
				{
					Displayname: "Bob",
					Email:       "bob@email.com",
					Password:    "cant_hack_me",
				},
				{
					Displayname: "Alice",
					Email:       "alice@email.com",
					Password:    "alice alice",
				},
				{
					Displayname: "Tom",
					Email:       "tom@email.com",
					Password:    "catt",
				},
				{
					Displayname: "Jerry",
					Email:       "jerry@email.com",
					Password:    "maus",
				},
			}
			u := USERS().As("u")
			tt.q = WithDefaultLog(Lverbose).
				InsertInto(u).
				Valuesx(func(col *Column) {
					for _, user := range users {
						col.SetString(u.DISPLAYNAME, user.Displayname)
						col.SetString(u.EMAIL, user.Email)
						col.SetString(u.PASSWORD, user.Password)
					}
				}).
				Returning(u.USER_ID)
			tt.wantQuery = "INSERT INTO public.users AS u (displayname, email, password)" +
				" VALUES ($1, $2, $3), ($4, $5, $6), ($7, $8, $9), ($10, $11, $12)" +
				" RETURNING u.user_id"
			tt.wantArgs = []interface{}{
				users[0].Displayname, users[0].Email, users[0].Password,
				users[1].Displayname, users[1].Email, users[1].Password,
				users[2].Displayname, users[2].Email, users[2].Password,
				users[3].Displayname, users[3].Email, users[3].Password,
			}
			return tt
		}(),
		func() TT {
			var tt TT
			tt.description = "ToSQL ColumnMapper panic translates to empty query and panicked value in args"
			user := User{}
			u := USERS().As("u")
			var errEmptyEmail = errors.New("email cannot be empty")
			tt.q = WithDefaultLog(Lverbose).
				InsertInto(u).
				Valuesx(func(col *Column) {
					if user.Email == "" {
						panic(errEmptyEmail)
					}
					col.SetString(u.DISPLAYNAME, user.Displayname)
					col.SetString(u.EMAIL, user.Email)
					col.SetString(u.PASSWORD, user.Password)
				}).
				Returning(u.USER_ID)
			tt.wantQuery = ""
			tt.wantArgs = []interface{}{errEmptyEmail}
			return tt
		}(),
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

func TestInsertQuery_Fetch(t *testing.T) {
	if testing.Short() {
		return
	}
	is := is.New(t)
	db, err := sql.Open("txdb", "InsertQuery_Fetch")
	is.NoErr(err)
	defer db.Close()
	u := USERS()

	// Missing DB
	err = InsertInto(u).
		ReturningRowx(func(row *Row) {}).
		Fetch(nil)
	is.True(err != nil)

	// SQL syntax error
	// use a tempDB so we don't foul up the current db transaction with the error
	tempDB, err := sql.Open("txdb", randomString(8))
	is.NoErr(err)
	var userID int
	err = WithDefaultLog(Linterpolate).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		ReturningRowx(func(row *Row) {
			row.ScanInto(&userID, u.USER_ID.Asc().NullsLast())
		}).
		Fetch(nil)
	is.True(err != nil)
	tempDB.Close()

	// No mapper
	err = WithDB(db).
		InsertInto(u).
		Fetch(nil)
	is.True(err != nil)

	// Empty mapper
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	err = WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		ReturningRowx(func(row *Row) {}).
		Fetch(nil)
	is.NoErr(err)
	tempDB.Close()

	// Wrong Scan type
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	err = WithDefaultLog(Lverbose).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		ReturningRowx(func(row *Row) {
			row.ScanInto(&userID, u.DISPLAYNAME)
		}).
		Fetch(nil)
	is.True(err != nil)
	tempDB.Close()

	// sql.ErrNoRows
	err = WithDefaultLog(Linterpolate).
		WithDB(db).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("Adviser01", "adviser01@u.nus.edu").
		OnConflict(u.DISPLAYNAME, u.EMAIL).DoNothing().
		ReturningRowx(func(row *Row) {
			row.Int(u.USER_ID)
		}).
		Fetch(nil)
	is.True(errors.Is(err, sql.ErrNoRows))

	// simulate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	err = WithDefaultLog(Lverbose).
		WithDB(db).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("Adviser01", "adviser01@u.nus.edu").
		OnConflict(u.DISPLAYNAME, u.EMAIL).DoNothing().
		ReturningRowx(func(row *Row) {}).
		FetchContext(ctx, nil)
	is.True(errors.Is(err, context.DeadlineExceeded))

	// RowMapper
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	var email string
	err = WithDefaultLog(Lverbose).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		ReturningRowx(func(row *Row) {
			email = row.String(u.EMAIL)
		}).
		Fetch(nil)
	is.NoErr(err)
	is.Equal("aaa@email.com", email)
	tempDB.Close()

	// Accumulator
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	var emails []string
	err = WithDefaultLog(Lverbose).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		Values("bbb", "bbb@email.com").
		Values("ccc", "ccc@email.com").
		Values("ddd", "ddd@email.com").
		Values("eee", "eee@email.com").
		Values("fff", "fff@email.com").
		Values("ggg", "ggg@email.com").
		Values("hhh", "hhh@email.com").
		Values("iii", "iii@email.com").
		Values("jjj", "jjj@email.com").
		Returningx(func(row *Row) {
			email = row.String(u.EMAIL)
		}, func() {
			emails = append(emails, email)
		}).
		Fetch(nil)
	is.NoErr(err)
	is.Equal(10, len(emails))
	tempDB.Close()

	// ColumnMapper
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	var userIDs []int
	users := []User{
		{
			Displayname: "Bob",
			Email:       "bob@email.com",
			Password:    "cant_hack_me",
		},
		{
			Displayname: "Alice",
			Email:       "alice@email.com",
			Password:    "alice alice",
		},
		{
			Displayname: "Tom",
			Email:       "tom@email.com",
			Password:    "catt",
		},
		{
			Displayname: "Jerry",
			Email:       "jerry@email.com",
			Password:    "maus",
		},
	}
	err = WithDefaultLog(Lverbose).
		WithDB(tempDB).
		InsertInto(u).
		Valuesx(func(col *Column) {
			for _, user := range users {
				col.SetString(u.DISPLAYNAME, user.Displayname)
				col.SetString(u.EMAIL, user.Email)
				col.SetString(u.PASSWORD, user.Password)
			}
		}).
		Returningx(func(row *Row) {
			userID = row.Int(u.USER_ID)
		}, func() {
			userIDs = append(userIDs, userID)
		}).
		Fetch(nil)
	is.NoErr(err)
	is.Equal(4, len(userIDs))
	tempDB.Close()

	// Panic with validation error in ColumnMapper
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	var errEmptyEmail = errors.New("email cannot be empty")
	user := User{} // Empty email
	_, err = WithDefaultLog(Lverbose).
		InsertInto(u).
		Valuesx(func(col *Column) {
			if user.Email == "" {
				panic(errEmptyEmail)
			}
			col.SetString(u.DISPLAYNAME, user.Displayname)
			col.SetString(u.EMAIL, user.Email)
			col.SetString(u.PASSWORD, user.Password)
		}).
		Exec(tempDB, 0)
	is.Equal(err, errEmptyEmail)
	tempDB.Close()

	// Panic with ExitPeacefully
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	emails = emails[:0]
	err = WithDefaultLog(Linterpolate).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		Returningx(func(row *Row) {
			email = row.String(u.EMAIL)
		}, func() {
			panic(ExitPeacefully)
		}).
		Fetch(nil)
	is.NoErr(err)
	is.Equal(0, len(emails))
	tempDB.Close()

	// Panic with any other ExitCode
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	emails = emails[:0]
	err = WithDefaultLog(Linterpolate).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		Returningx(func(row *Row) {
			email = row.String(u.EMAIL)
		}, func() {
			panic(ExitCode(1))
		}).
		Fetch(nil)
	is.True(errors.Is(err, ExitCode(1)))
	tempDB.Close()

	// Panic with error
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	ErrTest := errors.New("this is a test error")
	emails = emails[:0]
	err = WithDefaultLog(Linterpolate).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		Returningx(func(row *Row) {
			email = row.String(u.EMAIL)
		}, func() {
			panic(ErrTest)
		}).
		Fetch(nil)
	is.True(errors.Is(err, ErrTest))
	tempDB.Close()

	// Panic with 0
	tempDB, err = sql.Open("txdb", randomString(8))
	is.NoErr(err)
	emails = emails[:0]
	err = WithDefaultLog(Linterpolate).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		Returningx(func(row *Row) {
			email = row.String(u.EMAIL)
		}, func() {
			panic(0)
		}).
		Fetch(nil)
	is.Equal(fmt.Errorf("0").Error(), err.Error())
	tempDB.Close()
}

func TestInsertQuery_Exec(t *testing.T) {
	if testing.Short() {
		return
	}
	is := is.New(t)
	db, err := sql.Open("txdb", "DeleteQuery_Exec")
	is.NoErr(err)
	defer db.Close()
	u := USERS()

	// Missing DB
	_, err = InsertInto(u).
		ReturningRowx(func(row *Row) {}).
		Exec(nil, ErowsAffected)
	is.True(err != nil)

	// SQL syntax error
	// use a tempDB so we don't foul up the current db transaction with the error
	tempDB, err := sql.Open("txdb", randomString(8))
	is.NoErr(err)
	_, err = WithDefaultLog(Linterpolate).
		WithDB(tempDB).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		Returning(Fieldf("ERROR")).
		Exec(nil, ErowsAffected)
	is.True(err != nil)
	tempDB.Close()

	// simulate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	_, err = WithDB(db).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		ExecContext(ctx, nil, ErowsAffected)
	is.True(errors.Is(err, context.DeadlineExceeded))

	// rowsAffected
	rowsAffected, err := WithDefaultLog(Lverbose).
		WithDB(db).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		Exec(nil, ErowsAffected)
	is.NoErr(err)
	is.Equal(int64(1), rowsAffected)

	// again
	rowsAffected, err = WithDefaultLog(Lverbose).
		WithDB(db).
		InsertInto(u).
		Columns(u.DISPLAYNAME, u.EMAIL).
		Values("aaa", "aaa@email.com").
		OnConflict().DoNothing().
		Exec(nil, ErowsAffected)
	is.NoErr(err)
	is.Equal(int64(0), rowsAffected)
}
