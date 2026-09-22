#!/usr/bin/env python3
"""把 nginx 访问日志按国家汇总，写进 CloudWatch。

服务器上的位置：/opt/erp/region-metrics.py，由 systemd timer 每分钟跑一次。
装它的是 deploy/aws/06-region-metrics.sh。

-------------------------------------------------------------------------
先说清楚这份指标**量不到什么**，免得有人拿它当"用户等了多久"

nginx 的 $request_time 结束于「把响应交给内核套接字」，不是「客户端收到」。
2026-09-22 实测：中国的 IP 和美国的 IP，$request_time 和 $upstream_response_time
几乎完全相等——过太平洋那几百毫秒**一点都没被记进去**。

所以这里**不发布任何叫"延迟"的指标**。发布的是：

  Requests       请求数
  ClientAborts   499：客户端等不及自己断开——**这才是"慢到不能用"的真实信号**
  ServerErrors   5xx
  UpstreamSeconds 服务端自己算了多久（和用户在哪儿无关，但能看出接口本身慢没慢）
  BytesSent      发出去多少字节（压缩回归了会在这儿露头）

ClientAborts 按国家分，正是 2026-09-21 那次问题的信号：江苏那个 IP 0.131%、
美国 0.008%，差二十倍；压缩上线后中国降到 0。有了它，下次再有这种事，是告警
先说话，不是员工先说话。
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time

LOG = "/var/log/nginx/access.log"
STATE = "/var/lib/erp-region/state.json"
IPCACHE = "/var/lib/erp-region/ipcache.json"
NAMESPACE = "ERP/Web"
REGION = os.environ.get("AWS_REGION", "us-west-2")

# 一次最多读多少字节。正常一分钟的日志是几十 KB，落后 8 MB 说明中间停过
# （机器重启、定时器被停）。那时**跳到末尾**而不是补历史：补出来的几万行会
# 被全部算进"这一分钟"，画成一根根本没发生过的尖峰。
MAX_BYTES = 8 * 1024 * 1024

# 只分三档。
#
# 每多一个国家就多五个 CloudWatch 自定义指标（$0.30/个/月），而秘鲁来的那
# 几十次请求不值一块钱——它们和德国、日本一起进 OTHER 就够了。真有第二个
# 国家的人天天用，那时再把它单列出来。
BUCKETS = {"CN": "CN", "US": "US"}
OTHER = "OTHER"

# nginx 那份日志的格式见 deploy/host-nginx-log.conf。
# 只认带 rt= 的行：不带的是 2026-09-22 之前的旧格式，混进来会让计数虚高。
LINE = re.compile(
    r'^(?P<ip>\S+) \S+ \S+ \[[^\]]+\] "[^"]*" (?P<status>\d{3}) (?P<bytes>\d+) '
    r'"[^"]*" "[^"]*" rt=(?P<rt>[\d.]+) urt=(?P<urt>[\d.-]*)'
)


def load(path, default):
    try:
        with open(path) as f:
            return json.load(f)
    except Exception:
        return default


def save(path, data):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w") as f:
        json.dump(data, f)
    os.replace(tmp, path)  # 原子替换：崩在半路也不会留下半个文件


def country_of(ip: str, cache: dict) -> str:
    """IP 属于哪一档。查不出来算 OTHER。

    结果进缓存：同一批日志里一个 IP 会出现几百次，而 geoiplookup 是个进程。
    缓存跨次保留，所以稳定下来之后几乎不再有新进程。
    """
    hit = cache.get(ip)
    if hit:
        return hit
    code = OTHER
    try:
        out = subprocess.run(
            ["geoiplookup", ip], capture_output=True, text=True, timeout=2
        ).stdout
        # "GeoIP Country Edition: CN, China"
        part = out.split(":", 1)[1].strip() if ":" in out else ""
        two = part.split(",", 1)[0].strip()
        if len(two) == 2:
            code = BUCKETS.get(two, OTHER)
    except Exception:
        pass
    cache[ip] = code
    return code


def collect():
    """读日志新增的部分，按国家汇总。"""
    try:
        st = os.stat(LOG)
    except FileNotFoundError:
        return {}

    state = load(STATE, {})
    # **第一次跑：直接跳到末尾，不补历史。**
    #
    # 2026-09-22 装上时踩过：没有状态文件就从 0 读起，把当天已经积下的
    # 二十多万行一次读完，全部算进"这一分钟"——CloudWatch 上看到的是一根
    # 三万五千请求的尖峰，而那一整天的分布被压成了一个点。
    # 指标是从装上这一刻开始的，补历史既不准也没人要。
    if not state:
        save(STATE, {"offset": st.st_size, "inode": st.st_ino})
        return {}
    offset = state.get("offset", 0)
    # 换了文件（轮转）就从头读。比 inode 更可靠的判据没有——文件名是一样的。
    if state.get("inode") != st.st_ino:
        offset = 0
    # 落后太多就直接跳到末尾，理由见 MAX_BYTES。
    if st.st_size - offset > MAX_BYTES:
        offset = st.st_size
    # 文件变短了 = 被清空过，重来。
    if offset > st.st_size:
        offset = 0

    agg: dict[str, dict] = {}
    cache = load(IPCACHE, {})
    with open(LOG, "rb") as f:
        f.seek(offset)
        chunk = f.read(st.st_size - offset)
        offset = f.tell()

    # 最后一行可能只写了一半（nginx 正在写）。留给下一次：把偏移退回最后一个
    # 换行处。不退的话，那半行这次解析失败、下次又从半截开始，永远丢。
    text = chunk.decode("utf-8", "replace")
    cut = text.rfind("\n")
    if cut < 0:
        save(STATE, {"offset": st.st_size - len(chunk), "inode": st.st_ino})
        return {}
    offset -= len(chunk) - len(text[: cut + 1].encode("utf-8", "replace"))
    text = text[: cut + 1]

    for line in text.splitlines():
        m = LINE.match(line)
        if not m:
            continue
        bucket = country_of(m.group("ip"), cache)
        a = agg.setdefault(
            bucket, {"n": 0, "abort": 0, "err": 0, "bytes": 0, "urt": {}}
        )
        a["n"] += 1
        status = int(m.group("status"))
        if status == 499:
            a["abort"] += 1
        elif status >= 500:
            a["err"] += 1
        a["bytes"] += int(m.group("bytes"))
        urt = m.group("urt")
        if urt and urt != "-":
            # 按值计数而不是存一长串：CloudWatch 一次最多收 150 个值，而一分钟
            # 几千条请求里不同的耗时值只有几十种（毫秒粒度）。
            key = round(float(urt), 3)
            a["urt"][key] = a["urt"].get(key, 0) + 1

    save(STATE, {"offset": offset, "inode": st.st_ino})
    # 缓存不让它无限长：IP 会换，留着几万个没用的只是浪费。
    if len(cache) > 5000:
        cache = {}
    save(IPCACHE, cache)
    return agg


def put(agg):
    if not agg:
        return
    data = []
    for bucket, a in agg.items():
        dim = [{"Name": "Region", "Value": bucket}]
        data.append({"MetricName": "Requests", "Dimensions": dim,
                     "Value": a["n"], "Unit": "Count"})
        data.append({"MetricName": "ClientAborts", "Dimensions": dim,
                     "Value": a["abort"], "Unit": "Count"})
        data.append({"MetricName": "ServerErrors", "Dimensions": dim,
                     "Value": a["err"], "Unit": "Count"})
        data.append({"MetricName": "BytesSent", "Dimensions": dim,
                     "Value": a["bytes"], "Unit": "Bytes"})
        if a["urt"]:
            # 取最常见的 150 个值：CloudWatch 一次调用的上限就是 150。
            # 按出现次数排序，砍掉的是长尾里最罕见的那些。
            top = sorted(a["urt"].items(), key=lambda kv: -kv[1])[:150]
            data.append({
                "MetricName": "UpstreamSeconds", "Dimensions": dim,
                "Values": [v for v, _ in top], "Counts": [c for _, c in top],
                "Unit": "Seconds",
            })
    # 一次最多 1000 个 datum，这里远不到；分批只是防着将来加桶。
    for i in range(0, len(data), 20):
        subprocess.run(
            ["aws", "cloudwatch", "put-metric-data", "--region", REGION,
             "--namespace", NAMESPACE,
             "--metric-data", json.dumps(data[i:i + 20])],
            check=True, capture_output=True, timeout=30,
        )


if __name__ == "__main__":
    try:
        put(collect())
    except Exception as e:  # 永远不要因为采指标把机器搞出别的毛病
        print(f"region-metrics: {e}", file=sys.stderr)
        sys.exit(0)
