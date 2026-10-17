"""Rough JS bracket balance checker (handles strings, template literals and comments) for files without Node."""
import sys

def check(path):
    src = open(path, encoding='utf-8').read()
    stack = []
    i, line = 0, 1
    pairs = {')': '(', ']': '[', '}': '{'}
    mode = None  # None, "'", '"', '`', '//', '/*'
    template_depth = []
    problems = []
    while i < len(src):
        c = src[i]
        nxt = src[i + 1] if i + 1 < len(src) else ''
        if c == '\n':
            line += 1
            if mode == '//':
                mode = None
            i += 1
            continue
        if mode in ("'", '"'):
            if c == '\\':
                i += 2
                continue
            if c == mode:
                mode = None
            i += 1
            continue
        if mode == '`':
            if c == '\\':
                i += 2
                continue
            if c == '`':
                mode = None
            elif c == '$' and nxt == '{':
                stack.append(('${', line))
                template_depth.append(len(stack))
                mode = None
                i += 2
                continue
            i += 1
            continue
        if mode == '//':
            i += 1
            continue
        if mode == '/*':
            if c == '*' and nxt == '/':
                mode = None
                i += 2
                continue
            i += 1
            continue
        if c == '/' and nxt == '/':
            mode = '//'
            i += 2
            continue
        if c == '/' and nxt == '*':
            mode = '/*'
            i += 2
            continue
        if c in ("'", '"', '`'):
            mode = c
            i += 1
            continue
        if c in '([{':
            stack.append((c, line))
        elif c in ')]}':
            if not stack:
                problems.append(f"line {line}: unexpected '{c}'")
            else:
                top, opened = stack.pop()
                if top == '${':
                    if c != '}':
                        problems.append(f"line {line}: '{c}' closes template expression opened at {opened}")
                    template_depth.pop()
                    mode = '`'
                elif pairs[c] != top:
                    problems.append(f"line {line}: '{c}' does not match '{top}' opened on line {opened}")
        i += 1
    for top, opened in stack:
        problems.append(f"unclosed '{top}' opened on line {opened}")
    return problems

def check_imports(path):
    """Flags an import statement started inside another, unfinished one (a syntax error the bracket check misses)."""
    problems = []
    open_import = None
    for number, line in enumerate(open(path, encoding='utf-8').read().splitlines(), start=1):
        stripped = line.strip()
        if open_import is not None and stripped.startswith('import '):
            problems.append(f"line {number}: import inside the unfinished import from line {open_import}")
        if stripped.startswith('import ') and '{' in stripped and '}' not in stripped:
            open_import = number
        elif open_import is not None and '}' in stripped:
            open_import = None
    return problems


for path in sys.argv[1:]:
    issues = check(path) + check_imports(path)
    print(path, "OK" if not issues else "")
    for issue in issues[:5]:
        print("   ", issue)
