#!/usr/bin/env python

BESSD_HOST = 'localhost'
BESSD_PORT = '10514'
S1UDEV = 's1u'
SGIDEV = 'sgi'
# for retrieving arp records
import arpreq
# for retrieving route entries
import iptools
from pyroute2 import IPDB
# for pkt generation
from scapy.all import *
# for signal handling
import signal


try:
    from pybess.bess import *
except ImportError:
    print('Cannot import the API module (pybess)')
    raise


bess = BESS()


class RouteEntry:
    def __init__(self):
        self.neighbor_ip = ' '
        self.local_ip = ' '
        self.iface = ' '
        self.prefix = ' '
        self.prefix_len = ' '

# for holding unresolved ARP queries
dict = {}


def mac2hex(mac):
    return int(mac.replace(':', ''), 16)


def send_ping(neighbor_ip):
    os.system('ping -c 1 ' + neighbor_ip)


def send_arp(neighbor_ip, src_mac, iface):
    pkt=Ether(dst="ff:ff:ff:ff:ff:ff")/ARP(pdst=neighbor_ip, hwsrc=src_mac)
    pkt.show()
    hexdump(pkt)
    sendp(pkt, iface=iface)


def link_modules(server, module, next_module):
    print('Linking %s module' % next_module)
    # Connect module to next_module
    response = server.connect_modules(module, next_module)
    if response.error.code != 0:
        print('Error connecting module %s to %s' % (module, next_module))


def link_route_module(server, module, last_module, gateway_mac, prefix, prefix_len):
    print('Adding route entry for %s' % module)
    # Pass routing entry to bessd's route module
    response = server.run_module_command(module,
                                         'add',
                                         'IPLookupCommandAddArg',
                                         {'prefix': prefix,
                                          'prefix_len': int(prefix_len),
                                          'gate': 0})
    if response.error.code != 0:
        print('Error inserting route entry for %s' % module)
        return
                    
    # Create Update module
    response = server.create_module('Update',
                                    module + '_EthMac_' + str(gateway_mac),
                                    {'fields': [{'offset': 0, 'size': 6, 'value': gateway_mac}]})
    if response.error.code != 0:
        print('Error creating module %s' % next_module)
        return
            
    # Connect Update module to route module
    link_modules(server, module, module + '_EthMac_' + str(gateway_mac))

    # Connect Update module to dpdk_out module
    link_modules(server, module + '_EthMac_' + str(gateway_mac), last_module)


def probe_addr_and_insert_module(local_ip, neighbor_ip, iface,
                                 prefix, prefix_len, src_mac):
    # Store entry if entry does not exist in ARP cache
    item = RouteEntry()
    item.neighbor_ip = neighbor_ip
    item.local_ip = local_ip
    item.iface = iface
    item.prefix = prefix
    item.prefix_len = prefix_len
    dict[item.neighbor_ip] = item
    print('Adding entry ' + item.neighbor_ip + ' in dict')
    #print(item.local_ip)
    #print(item.iface)
    #print(item.prefix)
    #print(item.prefix_len)
    #print(src_mac)

    # Probe ARP request by sending ping
    send_ping(item.neighbor_ip)

    # Probe ARP request
    ##send_arp(neighbor_ip, src_mac, item.iface)


# TODO - XXX: What if route is deleted. Need to add logic to de-link chained modules
def netlink_event_listener(ipdb, netlink_message, action):

    # If you get a netlink message, parse it
    msg = netlink_message

    if action == 'RTM_NEWROUTE':
        #print(action)
        for att in msg['attrs']:
            if 'RTA_DST' in att:
                # Fetch IP range
                prefix = att[1]
            if 'RTA_GATEWAY' in att:
                # Fetch gateway MAC address
                neighbor_ip = att[1]
                _mac = arpreq.arpreq(att[1])
                if not _mac:
                    gateway_mac = 0
                else:
                    gateway_mac = mac2hex(_mac)
            if 'RTA_OIF' in att:
                # Fetch interface name
                iface = ipdb.interfaces[int(att[1])].ifname

        # Fetch prefix_len
        prefix_len = msg['dst_len']

        # if mac is 0, send ARP request
        if gateway_mac == 0:
            for ipv4 in ipdb.interfaces[iface].ipaddr.ipv4:
                local_ip = ipv4[0]
                probe_addr_and_insert_module(local_ip, neighbor_ip, iface,
                                             prefix, prefix_len, ipdb.interfaces[iface].address)

        else:	# if gateway_mac is set
            # Pause bessd to avoid race condition (and potential crashes)
            bess.pause_all()

            link_route_module(bess, iface + "_routes", iface + "_dpdk_po", gateway_mac, prefix, prefix_len)

            # Now resume bessd operations
            bess.resume_all()

    if action == 'RTM_NEWNEIGH':
        #print(action)
        #print(msg)
        for att in msg['attrs']:
            #print(att)
            if 'NDA_DST' in att:
                prefix = att[1]
                #print('prefix is ' + prefix)
            if 'NDA_LLADDR' in att:
                gateway_mac = att[1]
                #print('mac is ' + gateway_mac)
        item = dict.get(prefix)
        if item:
            print('Found an item with key ' + item.neighbor_ip)
            print('Linking module ' + item.iface + '_routes' + ' with ' + item.iface + '_dpdk_po ' + gateway_mac)
            #print("Prefix len: " + str(item.prefix_len))
            #print("Prefix: " + item.prefix)

            # Pause bessd to avoid race condition (and potential crashes)
            bess.pause_all()

            link_route_module(bess, item.iface + "_routes", item.iface + "_dpdk_po", mac2hex(gateway_mac), item.prefix, str(item.prefix_len))

            # Now resume bessd operations
            bess.resume_all()

            del dict[prefix]


def main():
    ipdb = IPDB()

    # Connect to BESS (assuming host=localhost, port=10514 (default))
    bess.connect(grpc_url=BESSD_HOST + ':' + BESSD_PORT)

    # Pause bessd to avoid race condition (and potential crashes)
    bess.pause_all()

    for i in ipdb.routes:
        # For every gateway entry
        if iptools.ipv4.validate_cidr(i['dst']) and i['gateway']:
            # Get interface name
            iface = ipdb.interfaces[int(i['oif'])].ifname
            # Get prefix
            prefix = i['dst'].split('/')[0]
            # Get prefix length
            prefix_len = i['dst'].split('/')[1]
            # Get MAC address of the the gateway
            _mac = arpreq.arpreq(i['gateway'])
            if _mac:
                gateway_mac = mac2hex(_mac)
                if iface == S1UDEV:
                    link_route_module(bess, iface + "_routes", iface + "_dpdk_po", gateway_mac, prefix, prefix_len)
                if iface == SGIDEV:
                    link_route_module(bess, iface + "_routes", iface + "_dpdk_po", gateway_mac, prefix, prefix_len)
            else:
                for ipv4 in ipdb.interfaces[int(i['oif'])].ipaddr.ipv4:
                    local_ip = ipv4[0]
                    probe_addr_and_insert_module(local_ip, i['gateway'], iface,
                                                 prefix, prefix_len, ipdb.interfaces[iface].address)

    # Now resume bessd operations
    bess.resume_all()

    event_callback = ipdb.register_callback(netlink_event_listener)

    def cleanup(*args):
        ipdb.unregister_callback(netlink_event_listener)
        sys.exit()

    signal.signal(signal.SIGINT, cleanup)
    signal.signal(signal.SIGTERM, cleanup)
    signal.pause()


if __name__ == '__main__':
    main()

