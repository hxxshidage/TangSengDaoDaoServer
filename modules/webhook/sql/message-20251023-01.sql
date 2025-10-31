-- +migrate Up

-- 业务查询消息记录表
CREATE TABLE `t_message_records` (
     `id` int NOT NULL AUTO_INCREMENT,
     `message_id` varchar(20) NOT NULL DEFAULT '',
     `message_seq` bigint NOT NULL DEFAULT '0',
     `client_msg_no` varchar(100) NOT NULL DEFAULT '',
     `setting` smallint NOT NULL DEFAULT '0',
     `signal` smallint NOT NULL DEFAULT '0',
     `header` varchar(100) NOT NULL DEFAULT '',
     `from_uid` varchar(40) NOT NULL DEFAULT '',
     `channel_id` varchar(100) NOT NULL DEFAULT '',
     `channel_type` smallint NOT NULL DEFAULT '0',
     `content_type` smallint NOT NULL DEFAULT '0',
     `payload` mediumblob NOT NULL,
     `cts` bigint NOT NULL,
     PRIMARY KEY (`id`),
     UNIQUE KEY `uidx_msgid` (`message_id`),
     KEY `midx_ctime_fid_cid` (`cts`,`from_uid`,`channel_id`)
) ENGINE=InnoDB AUTO_INCREMENT=14 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='消息记录表';