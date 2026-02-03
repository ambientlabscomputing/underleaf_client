#!/usr/bin/env python3
"""Quick mDNS service browser to verify Underleaf announcements."""

try:
    from zeroconf import Zeroconf, ServiceBrowser, ServiceListener
    import time
    
    class UnderleafListener(ServiceListener):
        def __init__(self):
            self.services = []
        
        def add_service(self, zeroconf, service_type, name):
            info = zeroconf.get_service_info(service_type, name)
            print(f"✓ Found: {name}")
            if info:
                print(f"  Address: {info.parsed_addresses()}")
                print(f"  Port: {info.port}")
                print(f"  Properties: {info.properties}")
            self.services.append(name)
        
        def remove_service(self, zeroconf, service_type, name):
            print(f"✗ Removed: {name}")
        
        def update_service(self, zeroconf, service_type, name):
            print(f"↻ Updated: {name}")
    
    print("Browsing for _underleaf._tcp.local services...")
    print("=" * 60)
    
    zeroconf = Zeroconf()
    listener = UnderleafListener()
    browser = ServiceBrowser(zeroconf, "_underleaf._tcp.local.", listener)
    
    print("Listening for 8 seconds...")
    time.sleep(8)
    
    zeroconf.close()
    
    print("=" * 60)
    print(f"Total services found: {len(listener.services)}")
    
except ImportError:
    print("zeroconf library not installed")
    print("Run: pip3 install zeroconf")
    import sys
    sys.exit(1)
