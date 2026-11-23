package main

import "github.com/go-mist/mist/pg"

//go:generate go run github.com/go-mist/mist/cmd/mist generate

// Task is one row of the tasks table.
type Task struct {
	ID    int64  `mist:"id,bigserial,pk" json:"id"`
	Title string `mist:"title,text,notnull" json:"title"`
	Done  bool   `mist:"done,boolean,notnull,default=false" json:"done"`
}

func (Task) Table() pg.TableMeta { return pg.TableMeta{Name: "tasks"} }

// Event records a change to a task. add and done write the task and an event in one transaction.
type Event struct {
	ID     int64  `mist:"id,bigserial,pk"`
	TaskID int64  `mist:"task_id,bigint,notnull,references=tasks.id"`
	Action string `mist:"action,text,notnull"`
}

func (Event) Table() pg.TableMeta { return pg.TableMeta{Name: "task_events"} }
