-- 添加系统密钥参数到参数字典
-- 用于支持混合认证功能

INSERT INTO sys_params (
    param_code,
    param_value,
    param_type,
    remark,
    creator,
    create_date,
    updater,
    update_date
) VALUES (
    'server.secret',
    'cc36d3ca-b065-47ef-ae29-d34f7540abd1',
    1,
    '系统密钥，用于内部服务间认证。与xiaozhi-backend-server配置中的secret保持一致',
    0,
    NOW(),
    0,
    NOW()
) ON DUPLICATE KEY UPDATE
    param_value = VALUES(param_value),
    remark = VALUES(remark),
    updater = VALUES(updater),
    update_date = VALUES(update_date);
