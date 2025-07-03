-- create the databases
CREATE
DATABASE IF NOT EXISTS `task`;

-- create the users for each database
CREATE
USER 'task'@'%' IDENTIFIED BY 'task';
GRANT CREATE
, ALTER
, INDEX, LOCK TABLES, REFERENCES,
UPDATE,
DELETE
, DROP
,
SELECT,
INSERT
ON `task`.* TO 'task'@'%';

FLUSH
PRIVILEGES;