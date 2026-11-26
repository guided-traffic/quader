# -*- coding: utf-8 -*-
from __future__ import annotations


class ModuleDocFragment(object):
    # The connection options every module shares.
    DOCUMENTATION = r'''
options:
  api_url:
    description:
      - The address of the API.
    type: str
    required: true
  timeout:
    description: Seconds to wait for an answer.
    type: int
    default: 30
notes:
  - The fragment's note.
requirements:
  - requests
seealso:
  - module: acme.tools.ping_host
attributes:
  check_mode:
    description: Can run in check mode.
    support: full
'''

    VALIDATE = r'''
options:
  validate:
    description: A command that checks the file.
    type: str
'''
