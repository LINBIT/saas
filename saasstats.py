#!/usr/bin/env python3

import argparse
import io
import json
import subprocess

import geoip2.database

georeadercity = geoip2.database.Reader('./GeoLite2-City.mmdb')
georeadercountry = geoip2.database.Reader('./GeoLite2-Country.mmdb')

parser = argparse.ArgumentParser(description='Process saas journal entries')
parser.add_argument('--sortby', help='sort output by property',
                    default='country', choices=('country', 'count'))
parser.add_argument('--drbd-version', help='filter by drbd-version')
args = parser.parse_args()

proc = subprocess.Popen(['journalctl', '-u', 'saas', '--output=cat'], stdout=subprocess.PIPE)
locations = {}
for line in io.TextIOWrapper(proc.stdout, encoding='utf-8'):
    ip = None
    try:
        linfo = json.loads(line)
        # remoteAddr contains IP:Port
        ip = linfo['remoteAddr'].split(':')[0].strip()
        if args.drbd_version and linfo['drbdversion'] != args.drbd_version:
            continue
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


sortby = lambda k: locations[k]['country_name']
reverse = False
if args.sortby == 'count':
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
