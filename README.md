# domain nft set
運行在OpenWrt 基於域名關鍵詞添加nftable set

## download
```shell
wget -O /opt/shieldlink https://raw.githubusercontent.com/hub66899/shield-link/master/shieldlink
chmod 777 /opt/shieldlink
```

## 配置
```shell
cat > /etc/init.d/shieldlink << EOF
#!/bin/sh /etc/rc.common
START=99
USE_PROCD=1
start_service() {
    procd_open_instance
    procd_set_param command "/opt/shieldlink"
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
EOF
chmod +x /etc/init.d/shieldlink
```

## 運行一次 自動創建配置文件

```shell
/opt/shieldlink
```

## 啟動

```shell
/etc/init.d/shieldlink enable
/etc/init.d/shieldlink start
```

配置dnsmasq的上游地址为127.0.0.1#5353
