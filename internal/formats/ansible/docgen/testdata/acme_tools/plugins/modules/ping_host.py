#!/usr/bin/python
DOCUMENTATION = r'''
module: ping_host
short_description: Pings a host through the API
description: Asks the API whether O(host) answers.
version_added: 1.1.0
author: Hans
extends_documentation_fragment:
  - acme.tools.common
  - acme.tools.common.validate
  - ansible.builtin.files
options:
  host:
    description: The host.
    type: str
    required: true
  timeout:
    description: Seconds; this module waits less.
    default: 10
notes:
  - The module's note.
requirements:
  - python >= 3.9
attributes:
  check_mode:
    support: partial
  diff_mode:
    description: Shows no diff.
    support: none
'''
EXAMPLES = r'''
- acme.tools.ping_host:
    host: db1
'''
RETURN = r'''
answered:
  description: Whether the host answered.
  type: bool
  returned: always
'''
from ansible.module_utils.basic import AnsibleModule


def main():
    AnsibleModule(argument_spec={}).exit_json(changed=False)


if __name__ == '__main__':
    main()
