-- +migrate Up

ALTER TABLE `user`
    ADD COLUMN `avatar` varchar(1024) NOT NULL DEFAULT '' AFTER `password`;

ALTER TABLE `user`
    MODIFY COLUMN `zone` varchar (20) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' AFTER `avatar`,
    MODIFY COLUMN `phone` varchar (100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' AFTER `zone`;