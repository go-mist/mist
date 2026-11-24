CREATE TABLE `tasks` (
	`id` bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
	`title` text NOT NULL,
	`done` tinyint(1) NOT NULL DEFAULT 0
);
--> statement-breakpoint
CREATE TABLE `task_events` (
	`id` bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
	`task_id` bigint NOT NULL,
	`action` text NOT NULL,
	CONSTRAINT `task_events_task_id_fkey` FOREIGN KEY (`task_id`) REFERENCES `tasks` (`id`)
);
