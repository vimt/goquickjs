let d = new Date(NaN); try { d.toISOString(); 'no' } catch (e) { e.name }
