#!/usr/bin/env python3

import io
import json
import os.path
import subprocess
import sys

import geoip2.database

georeadercity = geoip2.database.Reader('./GeoLite2-City.mmdb')
georeadercountry = geoip2.database.Reader('./GeoLite2-Country.mmdb')

proc = subprocess.Popen(['journalctl', '-u', 'saas', '--output=cat'], stdout=subprocess.PIPE)
locations = {}
for line in io.TextIOWrapper(proc.stdout, encoding='utf-8'):
    ip = None
    try:
        linfo = json.loads(line)
        # remoteAddr contains IP:Port
        ip = linfo['remoteAddr'].split(':')[0].strip()
    except Exception:
        continue

    if ip not in locations:
        locations[ip] = {'count': 0}
        georesponsecity = georeadercity.city(ip)
        georesponsecountry = georeadercountry.country(ip)
        locations[ip].update({
            'country_name': georesponsecountry.country.name,
            'subdivision_name': georesponsecity.subdivisions.most_specific.name,
            'city': georesponsecity.city.name,
        })
    locations[ip]['count'] += 1


# default sort
sortby = lambda k: locations[k]['country_name']
reverse = False
# lazy argparse
if len(sys.argv) == 2:
    if sys.argv[1] == '-h' or sys.argv[1] == '--help':
        print("{} [--sortby=[country|count] (default is 'country')]".format(os.path.basename(sys.argv[0])))
        sys.exit(0)
    elif sys.argv[1] == '--sortby=count':
        sortby = lambda k: int(locations[k]['count'])
        reverse = True

counts = 0
keys = sorted(locations.keys(), key=sortby, reverse=reverse)
for k in keys:
    v = locations[k]
    count = int(v['count'])
    counts += count
    print('{:15} ({:4}) {}/{}/{}'.format(k, count, v['country_name'], v['subdivision_name'], v['city']))

print('Sum:', counts)
