package main

import "github.com/go-mist/mist"

//go:generate go run github.com/go-mist/mist/cmd/mist generate

// Task is one row of the tasks table.
// The tag names the column. The Go type supplies the SQL type: int64 is bigint, string is text, bool is boolean.
type Task struct {
	ID    int64  `mist:"id,pk" json:"id"`
	Title string `mist:"title,notnull" json:"title"`
	Done  bool   `mist:"done,notnull,default=false" json:"done"`
}

func (Task) Table() mist.TableMeta { return mist.TableMeta{Name: "tasks"} }

// Event records a change to a task. add and done write the task and an event in one transaction.
type Event struct {
	ID     int64  `mist:"id,pk"`
	TaskID int64  `mist:"task_id,notnull,references=tasks.id"`
	Action string `mist:"action,notnull"`
}

func (Event) Table() mist.TableMeta { return mist.TableMeta{Name: "task_events"} }
