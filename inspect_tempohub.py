"""Read selected local TempoHub bytecode for interoperability research; never executes it."""
import io
import sys
from pathlib import Path
from PyInstaller.archive.readers import CArchiveReader
from xdis.unmarshal import load_code
from xdis import Bytecode
from xdis.opcodes import opcode_39 as opc

archive = CArchiveReader(r'C:\Program Files (x86)\EDIFIER TempoHub\EDIFIER TempoHub.exe')
pyz = archive.open_embedded_archive('PYZ.pyz')
magic = int.from_bytes(archive.extract('PYZ.pyz')[4:6], 'little')
out = Path('research')
out.mkdir(exist_ok=True)

def dump(code, f, level=0):
    f.write('\n' + '  '*level + 'FUNCTION ' + code.co_name + '\n')
    f.write('NAMES ' + repr(code.co_names) + '\n')
    f.write('CONSTANTS ' + repr([v for v in code.co_consts if not hasattr(v, 'co_code')]) + '\n')
    f.write(Bytecode(code, opc).dis() + '\n')
    for value in code.co_consts:
        if hasattr(value, 'co_code'):
            dump(value, f, level+1)

for name in pyz.toc:
    if name.startswith(('app.', 'driver.', 'interface.')) and any(term in name for term in sys.argv[1:]):
        code = load_code(io.BytesIO(pyz.extract(name, raw=True)), magic, code_objects={})
        path = out / (name + '.txt')
        with path.open('w', encoding='utf-8') as f:
            dump(code, f)
        print(path)
