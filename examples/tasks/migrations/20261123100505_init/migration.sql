CREATE TABLE "tasks" (
	"id" bigserial PRIMARY KEY,
	"title" text NOT NULL,
	"done" boolean NOT NULL DEFAULT false
);
--> statement-breakpoint
CREATE TABLE "task_events" (
	"id" bigserial PRIMARY KEY,
	"task_id" bigint NOT NULL,
	"action" text NOT NULL,
	CONSTRAINT "task_events_task_id_fkey" FOREIGN KEY ("task_id") REFERENCES "tasks" ("id")
);
