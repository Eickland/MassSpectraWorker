import matplotlib.pyplot as plt
import io

import HumSpectra.mass_spectra.visual.visual as visual

format_map = {
    'png': 'png',
    'svg': 'svg',
    'pdf': 'pdf',
    'jpeg': 'jpeg',
    'jpg': 'jpeg'
}

def _fig_to_bytes(fig, format='png', dpi=100):
    """Конвертирует фигуру matplotlib в байты"""
    buf = io.BytesIO()
    fig.savefig(buf, format=format, dpi=dpi, bbox_inches='tight')
    buf.seek(0)
    return buf.getvalue()

def plot_preview(ms_spectra,dpi=100,sizes=(8,50),width=10,height=8):
    
    fig, axes = plt.subplots(1,2,dpi=dpi,figsize=(width,height))
    
    visual.spectrum(ms_spectra,ax=axes[0])
    visual.vk(ms_spectra,sizes=sizes,ax=axes[1])
    
    fig.suptitle(ms_spectra.attrs['name'])
    
    return fig

