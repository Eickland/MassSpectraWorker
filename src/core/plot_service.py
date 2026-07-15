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

def plot_preview(ms_spectra,dpi=100,sizes=(8,50)):
    
    fig, axes = plt.subplots(1,2,dpi=dpi)
    
    visual.spectrum(ms_spectra,ax=axes[0])
    visual.vk(ms_spectra,sizes=sizes,ax=axes[1])
    
    fig.suptitle(ms_spectra.attrs['name'])
    
    return fig

def response_bytes_image(ms_spectra,format='png',dpi=100):
    
    fig = plot_preview(ms_spectra=ms_spectra,dpi=dpi)
    
    fmt = format_map.get(format, 'png')

    image_bytes = _fig_to_bytes(fig, format=fmt, dpi=dpi)
    
    plt.close(fig)
    
    mime_types = {
        'png': 'image/png',
        'svg': 'image/svg+xml',
        'pdf': 'application/pdf',
        'jpeg': 'image/jpeg'
    }
