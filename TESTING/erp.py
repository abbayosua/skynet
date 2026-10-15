from flask import Flask, render_template, request, redirect, session, jsonify
import sqlite3, hashlib
from datetime import datetime

app = Flask(__name__, template_folder='.')
app.secret_key = 'erp-secret-test'

DB = 'erp.db'

def init_db():
    conn = sqlite3.connect(DB)
    c = conn.cursor()
    c.executescript('''
        CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            username TEXT UNIQUE NOT NULL,
            password TEXT NOT NULL
        );
        CREATE TABLE IF NOT EXISTS products (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            sku TEXT UNIQUE NOT NULL,
            price REAL NOT NULL,
            stock INTEGER NOT NULL DEFAULT 0
        );
        CREATE TABLE IF NOT EXISTS customers (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            email TEXT
        );
        CREATE TABLE IF NOT EXISTS orders (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            customer_id INTEGER NOT NULL,
            total REAL NOT NULL,
            status TEXT NOT NULL DEFAULT 'new',
            created_at TEXT NOT NULL,
            FOREIGN KEY (customer_id) REFERENCES customers(id)
        );
        CREATE TABLE IF NOT EXISTS order_items (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            order_id INTEGER NOT NULL,
            product_id INTEGER NOT NULL,
            qty INTEGER NOT NULL,
            price REAL NOT NULL,
            FOREIGN KEY (order_id) REFERENCES orders(id),
            FOREIGN KEY (product_id) REFERENCES products(id)
        );
    ''')
    if not c.execute('SELECT id FROM users LIMIT 1').fetchone():
        pw = hashlib.sha256('admin123'.encode()).hexdigest()
        c.execute('INSERT INTO users (username, password) VALUES (?,?)', ('admin', pw))
    if not c.execute('SELECT id FROM products LIMIT 1').fetchone():
        products = [
            ('Laptop Pro 14', 'LAP-001', 15000000, 25),
            ('Keyboard Mech', 'KBD-002', 850000, 60),
            ('Mouse Wireless', 'MSE-003', 250000, 120),
            ('Monitor 27"', 'MON-004', 3200000, 15),
            ('SSD 1TB', 'SSD-005', 1750000, 40),
        ]
        c.executemany('INSERT INTO products (name, sku, price, stock) VALUES (?,?,?,?)', products)
        customers = [
            ('PT Maju Jaya', 'maju@example.com'),
            ('CV Berkah', 'berkah@example.com'),
            ('UD Sentosa', 'sentosa@example.com'),
        ]
        c.executemany('INSERT INTO customers (name, email) VALUES (?,?)', customers)
    conn.commit()
    conn.close()

init_db()

def get_db():
    conn = sqlite3.connect(DB)
    conn.row_factory = sqlite3.Row
    return conn

def render_tpl(template, **context):
    from jinja2 import Template
    return Template(template).render(**context)

# ---------- Auth ----------
@app.route('/login', methods=['GET', 'POST'])
def login():
    if request.method == 'POST':
        u = request.form['username']
        p = hashlib.sha256(request.form['password'].encode()).hexdigest()
        conn = get_db()
        user = conn.execute('SELECT * FROM users WHERE username=? AND password=?', (u, p)).fetchone()
        conn.close()
        if user:
            session['user'] = u
            return redirect('/')
        return render_tpl(TPL_LOGIN, error='Login gagal')
    return render_tpl(TPL_LOGIN, error='')

@app.route('/logout')
def logout():
    session.clear()
    return redirect('/login')

def require_login():
    if not session.get('user'):
        return redirect('/login')

# ---------- Dashboard ----------
@app.route('/')
def dashboard():
    if require_login():
        return require_login()
    conn = get_db()
    stats = {
        'products': conn.execute('SELECT COUNT(*) FROM products').fetchone()[0],
        'customers': conn.execute('SELECT COUNT(*) FROM customers').fetchone()[0],
        'orders': conn.execute('SELECT COUNT(*) FROM orders').fetchone()[0],
        'revenue': conn.execute('SELECT COALESCE(SUM(total),0) FROM orders WHERE status != "cancelled"').fetchone()[0],
        'stock_value': conn.execute('SELECT COALESCE(SUM(price*stock),0) FROM products').fetchone()[0],
        'low_stock': conn.execute('SELECT COUNT(*) FROM products WHERE stock < 20').fetchone()[0],
    }
    recent_orders = conn.execute('''
        SELECT o.id, c.name AS customer, o.total, o.status, o.created_at
        FROM orders o JOIN customers c ON c.id = o.customer_id
        ORDER BY o.created_at DESC LIMIT 5
    ''').fetchall()
    products = conn.execute('SELECT * FROM products ORDER BY stock ASC LIMIT 5').fetchall()
    conn.close()
    return render_tpl(TPL_DASH, stats=stats, orders=recent_orders, products=products)

# ---------- Products ----------
@app.route('/products')
def products():
    if require_login():
        return require_login()
    conn = get_db()
    rows = conn.execute('SELECT * FROM products ORDER BY id').fetchall()
    conn.close()
    return render_tpl(TPL_PRODUCTS, rows=rows)

@app.route('/products/add', methods=['POST'])
def product_add():
    if require_login():
        return require_login()
    conn = get_db()
    try:
        conn.execute('INSERT INTO products (name, sku, price, stock) VALUES (?,?,?,?)',
                     (request.form['name'], request.form['sku'], float(request.form['price']), int(request.form['stock'])))
        conn.commit()
    except sqlite3.IntegrityError as e:
        conn.close()
        return f'Error: SKU duplikat - {e}', 400
    conn.close()
    return redirect('/products')

@app.route('/products/delete/<int:id>')
def product_del(id):
    if require_login():
        return require_login()
    conn = get_db()
    conn.execute('DELETE FROM products WHERE id = ?', (id,))
    conn.commit()
    conn.close()
    return redirect('/products')

# ---------- Orders ----------
@app.route('/orders')
def orders():
    if require_login():
        return require_login()
    conn = get_db()
    rows = conn.execute('''
        SELECT o.id, c.name AS customer, o.total, o.status, o.created_at,
               (SELECT COUNT(*) FROM order_items oi WHERE oi.order_id = o.id) AS items
        FROM orders o JOIN customers c ON c.id = o.customer_id
        ORDER BY o.created_at DESC
    ''').fetchall()
    conn.close()
    return render_tpl(TPL_ORDERS, rows=rows)

@app.route('/orders/new', methods=['GET', 'POST'])
def order_new():
    if require_login():
        return require_login()
    conn = get_db()
    if request.method == 'POST':
        customer_id = int(request.form['customer_id'])
        product_ids = request.form.getlist('product_id')
        qtys = request.form.getlist('qty')
        total = 0
        order_items = []
        for pid, qty in zip(product_ids, qtys):
            pid, qty = int(pid), int(qty)
            prod = conn.execute('SELECT * FROM products WHERE id = ?', (pid,)).fetchone()
            if not prod or qty <= 0:
                continue
            if prod['stock'] < qty:
                conn.close()
                return f'Error: Stok {prod["name"]} tidak cukup (tersisa {prod["stock"]})', 400
            line_total = prod['price'] * qty
            total += line_total
            order_items.append((pid, qty, prod['price'], line_total))
        if not order_items:
            conn.close()
            return 'Error: tidak ada item valid', 400
        cur = conn.execute('INSERT INTO orders (customer_id, total, status, created_at) VALUES (?,?,?,?)',
                           (customer_id, total, 'new', datetime.now().isoformat()))
        order_id = cur.lastrowid
        for pid, qty, price, _ in order_items:
            conn.execute('INSERT INTO order_items (order_id, product_id, qty, price) VALUES (?,?,?,?)',
                         (order_id, pid, qty, price))
            conn.execute('UPDATE products SET stock = stock - ? WHERE id = ?', (qty, pid))
        conn.commit()
        conn.close()
        return redirect('/orders')
    customers = conn.execute('SELECT * FROM customers').fetchall()
    products = conn.execute('SELECT * FROM products WHERE stock > 0').fetchall()
    conn.close()
    return render_tpl(TPL_ORDER_NEW, customers=customers, products=products)

@app.route('/orders/status/<int:oid>', methods=['POST'])
def order_status(oid):
    if require_login():
        return require_login()
    status = request.form['status']
    conn = get_db()
    conn.execute('UPDATE orders SET status = ? WHERE id = ?', (status, oid))
    conn.commit()
    conn.close()
    return redirect('/orders')

# ---------- Templates ----------
TPL_LOGIN = '''
<!doctype html><title>Login ERP</title>
<style>
body{font-family:sans-serif;background:#f4f6f9;display:flex;justify-content:center;align-items:center;height:100vh}
.card{background:#fff;padding:40px;border-radius:12px;box-shadow:0 2px 10px rgba(0,0,0,.1);width:320px}
input{width:100%;padding:10px;margin:6px 0;border:1px solid #ddd;border-radius:6px;box-sizing:border-box}
input[type=submit]{background:#2f6fed;color:#fff;border:0;cursor:pointer;font-weight:bold}
.err{color:#e74c3c;font-size:14px}
</style>
<div class="card"><h2>ERP Login</h2>
{% if error %}<p class="err">{{error}}</p>{% endif %}
<form method=post>
<input type=text name=username placeholder=Username required>
<input type=password name=password placeholder=Password required>
<input type=submit value="Masuk">
</form><p style="font-size:12px;color:#999">default: admin / admin123</p></div>
'''

TPL_DASH = '''
<!doctype html><title>Dashboard ERP</title>
<style>
body{font-family:sans-serif;margin:0;background:#f4f6f9}
.nav{background:#1e293b;color:#fff;padding:14px 24px;display:flex;gap:24px;align-items:center}
.nav a{color:#cbd5e1;text-decoration:none;font-weight:600}
.nav a:hover{color:#fff}
.nav .spacer{flex:1}
.wrap{max-width:1100px;margin:24px auto;padding:0 24px}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:16px}
.card{background:#fff;border-radius:10px;padding:20px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
.card h3{margin:0 0 6px;font-size:13px;color:#64748b;text-transform:uppercase}
.card .val{font-size:26px;font-weight:700;color:#1e293b}
.card.warn{background:#fff7ed}
.panel{background:#fff;border-radius:10px;padding:20px;margin-top:20px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
table{width:100%;border-collapse:collapse}
th,td{padding:8px;text-align:left;border-bottom:1px solid #f1f5f9;font-size:14px}
th{color:#64748b;text-transform:uppercase;font-size:12px}
.badge{padding:3px 10px;border-radius:20px;font-size:12px;font-weight:600}
.b-new{background:#dbeafe;color:#1d4ed8}.b-paid{background:#dcfce7;color:#15803d}
.b-shipped{background:#fef9c3;color:#a16207}.b-cancelled{background:#fee2e2;color:#b91c1c}
</style>
<div class="nav"><strong>ERP SIMPLE</strong>
<a href="/">Dashboard</a><a href="/products">Produk</a><a href="/orders">Order</a>
<span class="spacer"></span><a href="/logout">Logout</a></div>
<div class="wrap">
<h1>Dashboard</h1>
<div class="cards">
<div class="card"><h3>Produk</h3><div class="val">{{stats.products}}</div></div>
<div class="card"><h3>Pelanggan</h3><div class="val">{{stats.customers}}</div></div>
<div class="card"><h3>Order</h3><div class="val">{{stats.orders}}</div></div>
<div class="card"><h3>Pendapatan</h3><div class="val">Rp{{'{:,}'.format(stats.revenue)}}</div></div>
<div class="card"><h3>Nilai Stok</h3><div class="val">Rp{{'{:,}'.format(stats.stock_value)}}</div></div>
<div class="card warn"><h3>Stok Menipis</h3><div class="val">{{stats.low_stock}}</div></div>
</div>
<div class="panel"><h3>Order Terbaru</h3>
<table><tr><th>ID</th><th>Pelanggan</th><th>Total</th><th>Status</th><th>Waktu</th></tr>
{% for o in orders %}
<tr><td>#{{o.id}}</td><td>{{o.customer}}</td><td>Rp{{'{:,}'.format(o.total)}}</td>
<td><span class="badge b-{{o.status}}">{{o.status}}</span></td><td>{{o.created_at}}</td></tr>
{% endfor %}</table></div>
<div class="panel"><h3>Stok Terendah</h3>
<table><tr><th>Produk</th><th>SKU</th><th>Stok</th><th>Harga</th></tr>
{% for p in products %}
<tr><td>{{p.name}}</td><td>{{p.sku}}</td><td>{{p.stock}}</td><td>Rp{{'{:,}'.format(p.price)}}</td></tr>
{% endfor %}</table></div>
</div>
'''

TPL_PRODUCTS = '''
<!doctype html><title>Produk</title>
<style>
body{font-family:sans-serif;margin:0;background:#f4f6f9}
.nav{background:#1e293b;color:#fff;padding:14px 24px;display:flex;gap:24px;align-items:center}
.nav a{color:#cbd5e1;text-decoration:none;font-weight:600}.nav a:hover{color:#fff}.nav .spacer{flex:1}
.wrap{max-width:1100px;margin:24px auto;padding:0 24px}
.panel{background:#fff;border-radius:10px;padding:20px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
table{width:100%;border-collapse:collapse}
th,td{padding:8px;text-align:left;border-bottom:1px solid #f1f5f9;font-size:14px}
th{color:#64748b;text-transform:uppercase;font-size:12px}
input{padding:8px;border:1px solid #ddd;border-radius:6px}
.btn{background:#2f6fed;color:#fff;border:0;padding:8px 14px;border-radius:6px;cursor:pointer;font-weight:600}
.del{color:#e74c3c;text-decoration:none}
.low{color:#e74c3c;font-weight:700}
</style>
<div class="nav"><strong>ERP SIMPLE</strong><a href="/">Dashboard</a><a href="/products">Produk</a><a href="/orders">Order</a><span class="spacer"></span><a href="/logout">Logout</a></div>
<div class="wrap"><h1>Manajemen Produk</h1>
<div class="panel">
<h3>Tambah Produk</h3>
<form method=post action="/products/add" style="display:flex;gap:8px;flex-wrap:wrap">
<input name=name placeholder="Nama" required><input name=sku placeholder="SKU" required>
<input name=price type=number step=0.01 placeholder="Harga" required>
<input name=stock type=number placeholder="Stok" required>
<button class=btn>Tambah</button></form></div>
<div class="panel" style="margin-top:16px">
<table><tr><th>ID</th><th>Nama</th><th>SKU</th><th>Harga</th><th>Stok</th><th></th></tr>
{% for p in rows %}
<tr><td>{{p.id}}</td><td>{{p.name}}</td><td>{{p.sku}}</td><td>Rp{{'{:,}'.format(p.price)}}</td>
<td class="{% if p.stock < 20 %}low{% endif %}">{{p.stock}}{% if p.stock < 20 %} ⚠{% endif %}</td>
<td><a class=del href="/products/delete/{{p.id}}" onclick="return confirm('Hapus?')">Hapus</a></td></tr>
{% endfor %}</table></div></div>
'''

TPL_ORDERS = '''
<!doctype html><title>Orders</title>
<style>
body{font-family:sans-serif;margin:0;background:#f4f6f9}
.nav{background:#1e293b;color:#fff;padding:14px 24px;display:flex;gap:24px;align-items:center}
.nav a{color:#cbd5e1;text-decoration:none;font-weight:600}.nav a:hover{color:#fff}.nav .spacer{flex:1}
.wrap{max-width:1100px;margin:24px auto;padding:0 24px}
.panel{background:#fff;border-radius:10px;padding:20px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
table{width:100%;border-collapse:collapse}
th,td{padding:8px;text-align:left;border-bottom:1px solid #f1f5f9;font-size:14px}
th{color:#64748b;text-transform:uppercase;font-size:12px}
.btn{background:#2f6fed;color:#fff;border:0;padding:6px 10px;border-radius:6px;cursor:pointer;font-weight:600}
select{padding:6px;border:1px solid #ddd;border-radius:6px}
.badge{padding:3px 10px;border-radius:20px;font-size:12px;font-weight:600}
.b-new{background:#dbeafe;color:#1d4ed8}.b-paid{background:#dcfce7;color:#15803d}
.b-shipped{background:#fef9c3;color:#a16207}.b-cancelled{background:#fee2e2;color:#b91c1c}
</style>
<div class="nav"><strong>ERP SIMPLE</strong><a href="/">Dashboard</a><a href="/products">Produk</a><a href="/orders">Order</a><span class="spacer"></span><a href="/logout">Logout</a></div>
<div class="wrap"><h1>Order</h1><a class="btn" href="/orders/new">+ Order Baru</a>
<div class="panel" style="margin-top:16px">
<table><tr><th>ID</th><th>Pelanggan</th><th>Items</th><th>Total</th><th>Status</th><th>Waktu</th><th>Update Status</th></tr>
{% for o in rows %}
<tr><td>#{{o.id}}</td><td>{{o.customer}}</td><td>{{o.items}}</td><td>Rp{{'{:,}'.format(o.total)}}</td>
<td><span class="badge b-{{o.status}}">{{o.status}}</span></td><td>{{o.created_at}}</td>
<td><form method=post action="/orders/status/{{o.id}}" style="display:flex;gap:4px">
<select name=status><option value=new {% if o.status=='new' %}selected{% endif %}>new</option>
<option value=paid {% if o.status=='paid' %}selected{% endif %}>paid</option>
<option value=shipped {% if o.status=='shipped' %}selected{% endif %}>shipped</option>
<option value=cancelled {% if o.status=='cancelled' %}selected{% endif %}>cancelled</option></select>
<button class=btn>Update</button></form></td></tr>
{% endfor %}</table></div></div>
'''

TPL_ORDER_NEW = '''
<!doctype html><title>Order Baru</title>
<style>
body{font-family:sans-serif;margin:0;background:#f4f6f9}
.nav{background:#1e293b;color:#fff;padding:14px 24px;display:flex;gap:24px;align-items:center}
.nav a{color:#cbd5e1;text-decoration:none;font-weight:600}.nav a:hover{color:#fff}.nav .spacer{flex:1}
.wrap{max-width:800px;margin:24px auto;padding:0 24px}
.panel{background:#fff;border-radius:10px;padding:20px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
select,input{padding:8px;border:1px solid #ddd;border-radius:6px;margin:4px 0}
.btn{background:#2f6fed;color:#fff;border:0;padding:10px 18px;border-radius:6px;cursor:pointer;font-weight:600}
.row{display:flex;gap:8px;align-items:center}
</style>
<div class="nav"><strong>ERP SIMPLE</strong><a href="/">Dashboard</a><a href="/products">Produk</a><a href="/orders">Order</a><span class="spacer"></span><a href="/logout">Logout</a></div>
<div class="wrap"><h1>Buat Order Baru</h1>
<div class="panel">
<form method=post>
<label>Pelanggan</label><br>
<select name=customer_id required>
{% for c in customers %}<option value="{{c.id}}">{{c.name}}</option>{% endfor %}</select><br><br>
<label>Item</label><br>
<div class="row"><select name=product_id>
{% for p in products %}<option value="{{p.id}}">{{p.name}} (stok {{p.stock}}) - Rp{{'{:,}'.format(p.price)}}</option>{% endfor %}</select>
<input name=qty type=number value=1 min=1 required style="width:80px"></div>
<div class="row"><select name=product_id>
{% for p in products %}<option value="{{p.id}}">{{p.name}} (stok {{p.stock}}) - Rp{{'{:,}'.format(p.price)}}</option>{% endfor %}</select>
<input name=qty type=number value=1 min=1 required style="width:80px"></div>
<br><button class=btn>Simpan Order</button></form>
</div></div>
'''

if __name__ == '__main__':
    app.run(debug=True, port=5556)