# domain nft set
運行在OpenWrt 基於域名關鍵詞添加nftable set

## download
```shell
wget -O /opt/vpnmanager https://raw.githubusercontent.com/hub66899/vpn-manager/master/vpnmanager
chmod 777 /opt/vpnmanager
```

## 配置
```shell
cat > /etc/init.d/vpnmanager << EOF
#!/bin/sh /etc/rc.common
START=99
USE_PROCD=1
start_service() {
    procd_open_instance
    procd_set_param command "/opt/vpnmanager"
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
EOF
chmod +x /etc/init.d/vpnmanager
```

## 運行一次 自動創建配置文件

```shell
/opt/vpnmanager
```

## 配置

配置/etc/vpnmanager中的yml配置文件

配置router策略路由

配置dnsmasq的上游地址為127.0.0.1#5353

## 啟動

```shell
/etc/init.d/vpnmanager enable
/etc/init.d/vpnmanager start
```
