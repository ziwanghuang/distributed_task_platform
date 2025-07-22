-- create the databases
CREATE DATABASE IF NOT EXISTS `task`
    CHARACTER SET utf8mb4
    COLLATE utf8mb4_0900_ai_ci;

-- create the users for each database
CREATE USER 'task'@'%' IDENTIFIED BY 'task';
GRANT CREATE, ALTER, INDEX, LOCK TABLES, REFERENCES, UPDATE, DELETE, DROP, SELECT, INSERT ON `task`.* TO 'task'@'%';
FLUSH PRIVILEGES;

